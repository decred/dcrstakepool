// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/wallet/udb"

	_ "github.com/go-sql-driver/mysql"
)

type appContext struct {
	sync.RWMutex
	// locking required
	ticketsMSA       map[chainhash.Hash]string            // [ticket]multisigaddr
	userVotingConfig map[string]userdata.UserVotingConfig // [multisigaddr]

	// no locking required
	blockheight        int64
	coldwalletextpub   *hdkeychain.ExtendedKey
	dataPath           string
	feeAddrs           map[string]struct{}
	nodeConnection     *dcrrpcclient.Client
	params             *chaincfg.Params
	wg                 sync.WaitGroup // wait group for go routine exits
	quit               chan struct{}
	reloadTickets      chan struct{}
	reloadUserConfig   chan struct{}
	userData           *userdata.UserData
	votingConfig       *VotingConfig
	walletConnection   *dcrrpcclient.Client
	winningTicketsChan chan WinningTicketsForBlock
	testing            bool // enabled only for testing
}

// VotingConfig contains global voting defaults.
type VotingConfig struct {
	VoteBits         uint16
	VoteVersion      uint32
	VoteBitsExtended string
}

type WinningTicketsForBlock struct {
	blockHash      *chainhash.Hash
	blockHeight    int64
	host           string
	winningTickets []*chainhash.Hash
}

var (
	cfg        *config
	errSuccess = errors.New("success")
)

// calculateFeeAddresses decodes the string of stake pool payment addresses
// to search incoming tickets for. The format for the passed string is:
// "xpub...:end"
// where xpub... is the extended public key and end is the last
// address index to scan to, exclusive. Effectively, it returns the derived
// addresses for this public key for the address indexes [0,end). The branch
// used for the derivation is always the external branch.
func calculateFeeAddresses(xpubStr string, params *chaincfg.Params) (map[string]struct{}, error) {
	end := uint32(10000)

	log.Infof("Please wait, deriving %v stake pool fees addresses "+
		"for extended public key %s", end, xpubStr)

	// Parse the extended public key and ensure it's the right network.
	key, err := hdkeychain.NewKeyFromString(xpubStr)
	if err != nil {
		return nil, err
	}
	if !key.IsForNet(params) {
		return nil, fmt.Errorf("extended public key is for wrong network")
	}

	// Derive from external branch
	branchKey, err := key.Child(udb.ExternalBranch)
	if err != nil {
		return nil, err
	}

	// Derive the addresses from [0, end) for this extended public key.
	// deriveChildAddresses takes the start index and the count.
	addrs, err := deriveChildAddresses(branchKey, 0, end, params)
	if err != nil {
		return nil, err
	}

	addrMap := make(map[string]struct{})
	for i := range addrs {
		addrMap[addrs[i].EncodeAddress()] = struct{}{}
	}

	return addrMap, nil
}

func deriveChildAddresses(key *hdkeychain.ExtendedKey, startIndex, count uint32, params *chaincfg.Params) ([]dcrutil.Address, error) {
	addresses := make([]dcrutil.Address, 0, count)
	for i := uint32(0); i < count; {
		child, err := key.Child(startIndex + i)
		if err == hdkeychain.ErrInvalidChild {
			continue
		}
		if err != nil {
			return nil, err
		}
		addr, err := child.Address(params)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, addr)
		i++
	}
	return addresses, nil
}

func runMain() int {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		return 1
	}
	cfg = loadedCfg
	dataPath := filepath.Join(cfg.DataDir, "data.json")

	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	log.Infof("Version: %s", version())
	log.Infof("Network: %s", activeNetParams.Params.Name)
	log.Infof("Home dir: %s", cfg.HomeDir)

	// Create the data directory in case it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		log.Errorf("unable to create data directory: %v", cfg.DataDir)
		return 2
	}

	feeAddrs, err := calculateFeeAddresses(cfg.ColdWalletExtPub,
		activeNetParams.Params)
	if err != nil {
		log.Errorf("Error calculating fee payment addresses: %v", err)
		return 2
	}

	dcrrpcclient.UseLogger(clientLog)

	var walletVer semver
	walletConn, walletVer, err := connectWalletRPC(cfg)
	if err != nil || walletConn == nil {
		log.Infof("Connection to dcrwallet failed: %v", err)
		return 2
	}
	log.Infof("Connected to dcrwallet (JSON-RPC API v%s)",
		walletVer.String())
	walletInfoRes, err := walletConn.WalletInfo()
	if err != nil || walletInfoRes == nil {
		log.Errorf("Unable to retrieve walletoinfo results")
		return 3
	}

	votingConfig := VotingConfig{
		VoteBits:         walletInfoRes.VoteBits,
		VoteBitsExtended: walletInfoRes.VoteBitsExtended,
		VoteVersion:      walletInfoRes.VoteVersion,
	}
	log.Infof("VotingConfig: VoteVersion %v VoteBits %v", votingConfig.VoteVersion,
		votingConfig.VoteBits)

	// TODO re-work main loop
	// should be something like this:
	// loadData()
	// if ticket/user voting prefs -> enable voting -> refresh
	// if no ticket/user voting prefs -> pull from db/wallets -> enable voting

	var userdata = &userdata.UserData{}
	userdata.DBSetConfig(cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	userVotingConfig, err := userdata.MySQLFetchUserVotingConfig()
	if err != nil {
		log.Infof("could not obtain voting config: %v", err)
		return 12 // wtf
	}

	ctx := &appContext{
		blockheight:        0,
		dataPath:           dataPath,
		feeAddrs:           feeAddrs,
		params:             activeNetParams.Params,
		quit:               make(chan struct{}),
		reloadUserConfig:   make(chan struct{}),
		reloadTickets:      make(chan struct{}),
		userData:           userdata,
		userVotingConfig:   userVotingConfig,
		votingConfig:       &votingConfig,
		walletConnection:   walletConn,
		winningTicketsChan: make(chan WinningTicketsForBlock),
		testing:            false,
	}

	ctx.ticketsMSA, _ = walletFetchUserTickets(ctx)

	// Daemon client connection
	nodeConn, nodeVer, err := connectNodeRPC(ctx, cfg)
	if err != nil || nodeConn == nil {
		log.Infof("Connection to dcrd failed: %v", err)
		return 6
	}
	ctx.nodeConnection = nodeConn

	// Display connected network
	curnet, err := nodeConn.GetCurrentNet()
	if err != nil {
		log.Errorf("Unable to get current network from dcrd: %v", err)
		return 7
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	_, height, err := nodeConn.GetBestBlock()
	if err != nil {
		log.Errorf("unable to get bestblock from dcrd: %v", err)
		return 8
	}
	ctx.blockheight = height

	if err = nodeConn.NotifyWinningTickets(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for  "+
			"winning tickets notifications: %s\n", err.Error())
		return 9
	}

	if !cfg.NoRPCListen {
		startGRPCServers(ctx.reloadUserConfig)
	}

	// Only accept a single CTRL+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Start waiting for the interrupt signal
	go func() {
		<-c
		signal.Stop(c)
		// Close the channel so multiple goroutines can get the message
		log.Infof("CTRL+C hit.  Closing goroutines.")
		//saveData(ctx)
		close(ctx.quit)
	}()

	ctx.wg.Add(3)
	go ctx.winningTicketHandler()
	go ctx.reloadTicketsHandler()
	go ctx.reloadUserConfigHandler()

	if cfg.NoRPCListen {
		// Initial reload of user voting config
		ctx.reloadUserConfig <- struct{}{}
		// Start reloading when a ticker fires
		userConfigTicker := time.NewTicker(time.Second * 240)
		go func() {
			for range userConfigTicker.C {
				ctx.reloadUserConfig <- struct{}{}
			}
		}()
	}

	// Wait for CTRL+C to signal goroutines to terminate via quit channel.
	ctx.wg.Wait()

	return 0
}

func main() {
	os.Exit(runMain())
}

// saveData saves all the global data to a file so they can be read back
// in at next run.
func saveData(ctx *appContext) {
	ctx.Lock()
	defer ctx.Unlock()

	w, err := os.Create(ctx.dataPath)
	if err != nil {
		log.Errorf("Error opening file %s: %v", ctx.dataPath, err)
		return
	}
	enc := json.NewEncoder(w)
	defer w.Close()
	if err := enc.Encode(&ctx); err != nil {
		log.Errorf("Failed to encode file %s: %v", ctx.dataPath, err)
		return
	}
}

// loadData loads the saved data from the saved file.  If empty, missing, or
// malformed file, just don't load anything and start fresh
func (ctx *appContext) loadData() {
	ctx.Lock()
	defer ctx.Unlock()
}

// winner contains all the bits and pieces required to vote and to print
// statistics after usage.
type winner struct {
	msa          string                    // multisig
	ticket       *chainhash.Hash           // ticket
	config       userdata.UserVotingConfig // voting config
	signDuration time.Duration
	sendDuration time.Duration
	duration     time.Duration // overall vote duration
	err          error         // log errors along the way
}

// vote Generates a vote and send it off to the network.  This is a go routine!
func (ctx *appContext) vote(wg *sync.WaitGroup, blockHash *chainhash.Hash, blockHeight int64, w *winner) {
	start := time.Now()

	defer func() {
		w.duration = time.Since(start)
		wg.Done()
	}()

	// Ask wallet to generate vote result.
	var res *dcrjson.GenerateVoteResult
	res, w.err = ctx.walletConnection.GenerateVote(blockHash, blockHeight,
		w.ticket, w.config.VoteBits, ctx.votingConfig.VoteBitsExtended)
	if w.err != nil {
		return
	}
	w.signDuration = time.Since(start)

	// Create raw transaction.
	var buf []byte
	buf, w.err = hex.DecodeString(res.Hex)
	if w.err != nil {
		return
	}
	newTx := wire.NewMsgTx()
	w.err = newTx.FromBytes(buf)
	if w.err != nil {
		return
	}

	// Ask wallet to transmit raw transaction.
	startSend := time.Now()
	_, w.err = ctx.nodeConnection.SendRawTransaction(newTx, false)
	w.sendDuration = time.Since(startSend)
}

// processWinningTickets is called every time a new block comes in to handle
// voting.  The function requires ASAP processing for each vote and therefore
// it is not sequential and hard to read.  This is unfortunate but a reality of
// speeding up code.
func (ctx *appContext) processWinningTickets(wt WinningTicketsForBlock) {
	start := time.Now()

	// We always have to reload so signal the other end on the way out.
	// Maybe we can change this to a go routine so that we are not gated on
	// the function finishing first.  Reason it is deferred now is to make
	// sure the wallet isn't busy while processing the higher priority
	// voting activity.
	defer func() {
		// Don't block on messaging.  We want to make sure we can
		// handle the next call ASAP.
		select {
		case ctx.reloadTickets <- struct{}{}:
		default:
			// We log this in order to detect if we potentially
			// have a deadlock.
			log.Infof("Reload tickets message not sent")
		}
	}()

	// We use pointer because it is the fastest accessor.
	winners := make([]*winner, 0, len(wt.winningTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	ctx.RLock()
	for _, ticket := range wt.winningTickets {
		// Look up multi sig address.
		msa, ok := ctx.ticketsMSA[*ticket]
		if !ok {
			log.Debugf("unmanaged winning ticket: %v", ticket)
			if ctx.testing {
				panic("boom")
			}
			continue
		}

		voteCfg, ok := ctx.userVotingConfig[msa]
		if !ok {
			// Use defaults if not found.
			log.Warnf("vote config not found for %v using defaults",
				msa)
			voteCfg = userdata.UserVotingConfig{
				Userid:          0,
				MultiSigAddress: msa,
				VoteBits:        ctx.votingConfig.VoteBits,
				VoteBitsVersion: ctx.votingConfig.VoteVersion,
			}
		} else {
			// If the user's voting config has a vote version that
			// is different from our global vote version that we
			// plucked from dcrwallet walletinfo then just use the
			// default votebits.
			if voteCfg.VoteBitsVersion !=
				ctx.votingConfig.VoteVersion {

				voteCfg.VoteBits = ctx.votingConfig.VoteBits
				log.Infof("userid %v multisigaddress %v vote "+
					"version mismatch user %v stakepoold "+
					"%v using votebits %d",
					voteCfg.Userid, voteCfg.MultiSigAddress,
					voteCfg.VoteBitsVersion,
					ctx.votingConfig.VoteVersion,
					voteCfg.VoteBits)
			}
		}

		w := &winner{
			msa:    msa,
			ticket: ticket,
			config: voteCfg,
		}
		winners = append(winners, w)

		// When testing we don't send the tickets.
		if ctx.testing {
			continue
		}

		wg.Add(1)
		go ctx.vote(&wg, wt.blockHash, wt.blockHeight, w)
	}
	ctx.RUnlock()

	wg.Wait()

	end := time.Now()

	// Log ticket information outside of the handler.
	go func() {
		var winnerCount, loserCount int
		for _, w := range winners {
			if w.err == nil {
				winnerCount++
				w.err = errSuccess
			} else {
				loserCount++
			}
			log.Infof("winning ticket %v msa %v duration %v (%v + %v [+ %v]): %v",
				w.ticket, w.msa, w.duration, w.signDuration, w.sendDuration,
				w.duration-w.signDuration-w.sendDuration, w.err)
		}
		log.Infof("processWinningTickets: height %v block %v "+
			"duration %v success %v failure %v", wt.blockHeight,
			wt.blockHash, end.Sub(start), winnerCount, loserCount)
	}()
}

func (ctx *appContext) reloadTicketsHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case <-ctx.reloadTickets:
			start := time.Now()
			newTickets, msg := walletFetchUserTickets(ctx)
			end := time.Now()

			// replace tickets
			ctx.Lock()
			ctx.ticketsMSA = newTickets
			ctx.Unlock()

			log.Infof("walletFetchUserTickets: %v %v", msg,
				end.Sub(start))
		case <-ctx.quit:
			return
		}
	}
}

func (ctx *appContext) reloadUserConfigHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case <-ctx.reloadUserConfig:
			start := time.Now()
			newUserConfig, err :=
				ctx.userData.MySQLFetchUserVotingConfig()
			end := time.Now()
			log.Infof("MySQLFetchUserVotingConfig: %v",
				end.Sub(start))

			if err != nil {
				log.Errorf("unable to reload user config due "+
					"to db error: %v", err)
				continue
			}

			// replace UserVotingConfig
			ctx.Lock()
			ctx.userVotingConfig = newUserConfig
			ctx.Unlock()
		case <-ctx.quit:
			return
		}
	}
}

func (ctx *appContext) winningTicketHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case wt := <-ctx.winningTicketsChan:
			go ctx.processWinningTickets(wt)
		case <-ctx.quit:
			return
		}
	}
}
