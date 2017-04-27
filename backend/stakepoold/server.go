// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
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
	VoteBitsExtended string
	VoteInfo         *dcrjson.GetVoteInfoResult
	VoteVersion      uint32
}

type WinningTicketsForBlock struct {
	blockHash      *chainhash.Hash
	blockHeight    int64
	host           string
	winningTickets []*chainhash.Hash
}

var (
	cfg *config
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

	defer backendLog.Flush()

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

	ctx.ticketsMSA = walletFetchUserTickets(ctx)

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

	startGRPCServers(ctx.reloadUserConfig)

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
		return
	}()

	ctx.wg.Add(3)
	go ctx.winningTicketHandler()
	go ctx.reloadTicketsHandler()
	go ctx.reloadUserConfigHandler()

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

func (ctx *appContext) sendTickets(blockHash, ticket *chainhash.Hash, msa string, height int64) error {
	sstx, err := walletCreateVote(ctx, blockHash, height, ticket, msa)
	if err != nil {
		return fmt.Errorf("failed to create vote: %v", err)
	}

	_, err = nodeSendVote(ctx, sstx)
	if err != nil {
		return fmt.Errorf("failed to vote: %v", err)
	}

	return nil
}

func (ctx *appContext) processWinningTickets(wt WinningTicketsForBlock) {
	// We always have to reload so signal the other end on the way out.
	// Maybe we can change this to a go routine so that we are not gated on
	// the function finishing first.  Reason it is defered now is to make
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

	for _, ticket := range wt.winningTickets {
		// Look up multi sig address.
		ctx.RLock()
		msa, ok := ctx.ticketsMSA[*ticket]
		ctx.RUnlock()
		if !ok {
			log.Debugf("unmanaged winning ticket: %v", ticket)
			if ctx.testing {
				panic("boom")
			}
			continue
		}

		log.Infof("winning ticket %v height %v block hash %v msa %v",
			ticket, wt.blockHeight, wt.blockHash, msa)

		// When testing we don't send the tickets.
		if ctx.testing {
			continue
		}

		err := ctx.sendTickets(wt.blockHash, ticket, msa, wt.blockHeight)
		if err != nil {
			log.Infof("%v", err)
		}
	}
}

func (ctx *appContext) reloadTicketsHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case <-ctx.reloadTickets:
			start := time.Now()
			newTickets := walletFetchUserTickets(ctx)
			end := time.Now()

			// replace tickets
			ctx.Lock()
			ctx.ticketsMSA = newTickets
			ctx.Unlock()

			log.Infof("walletFetchUserTickets: %v", end.Sub(start))
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
			newUserConfig, err := ctx.userData.MySQLFetchUserVotingConfig()
			end := time.Now()
			log.Infof("MySQLFetchUserVotingConfig: %v", end.Sub(start))

			if err != nil {
				log.Errorf("unable to reload user config due to db error: %v",
					err)
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
