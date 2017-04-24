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
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/backend/stakepoold/voteoptions"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/wallet/udb"

	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

type appContext struct {
	sync.RWMutex
	// locking required
	tickets          map[string]string           // [ticket]multisigaddr
	userVotingConfig map[string]UserVotingConfig // [multisigaddr]

	// no locking required
	blockheight        int64
	coldwalletextpub   *hdkeychain.ExtendedKey
	dataPath           string
	feeAddrs           map[string]struct{}
	nodeConnection     *dcrrpcclient.Client
	params             *chaincfg.Params
	wg                 sync.WaitGroup // wait group for go routine exits
	quit               chan struct{}
	reload             chan struct{}
	walletConnection   *dcrrpcclient.Client
	votingConfig       *VotingConfig
	winningTicketsChan chan WinningTicketsForBlock
	testing            bool // enabled only for testing

}

// UserVotingConfig contains per-user voting preferences.
type UserVotingConfig struct {
	Userid          int64
	MultiSigAddress string
	VoteBits        uint16
	VoteBitsVersion uint32
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
	userVotingConfig, err := dbFetchUserVotingConfig(cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
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
		reload:             make(chan struct{}),
		userVotingConfig:   userVotingConfig,
		walletConnection:   walletConn,
		votingConfig:       &votingConfig,
		winningTicketsChan: make(chan WinningTicketsForBlock),
		testing:            false,
	}

	ctx.tickets = walletFetchUserTickets(ctx)

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

	// Get voteinfo for VoteVersion from wallet
	voteInfo, err := nodeConn.GetVoteInfo(votingConfig.VoteVersion)
	if err = nodeConn.NotifyWinningTickets(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for  "+
			"winning tickets notifications: %s\n", err.Error())
		return 9
	}

	votingConfig.VoteInfo = voteInfo
	log.Infof("VotingConfig: VoteInfo %v",
		spew.Sdump(votingConfig.VoteInfo))

	voteinfoString, err := json.Marshal(votingConfig.VoteInfo)
	if err != nil {
		log.Errorf("unable to encode VoteInfo: %v", err)
		return 10
	}

	voCfg := &voteoptions.Config{
		VoteInfo:    string(voteinfoString),
		VoteVersion: votingConfig.VoteVersion,
	}
	vo, err := voteoptions.NewVoteOptions(voCfg)
	if err != nil {
		log.Errorf("NewVoteOptions failed: %v", err)
		return 11
	}

	startGRPCServers(vo)

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

	ctx.wg.Add(2)
	go ctx.winningTicketHandler()
	go ctx.reloadHandler()

	// Wait for CTRL+C to signal goroutines to terminate via quit channel.
	ctx.wg.Wait()

	return 0
}

func main() {
	os.Exit(runMain())
}

func dbFetchUserVotingConfig(user string, password string, hostname string, port string, database string) (map[string]UserVotingConfig, error) {
	var (
		Userid          int64
		MultiSigAddress string
		VoteBits        int64
		VoteBitsVersion int64
	)

	userInfo := map[string]UserVotingConfig{}

	db, err := sql.Open("mysql", fmt.Sprint(user, ":", password, "@(", hostname, ":", port, ")/", database, "?charset=utf8mb4"))
	if err != nil {
		log.Errorf("Unable to open db: %v", err)
		return userInfo, err
	}

	// sql.Open just validates its arguments without creating a connection
	// Verify that the data source name is valid with Ping:
	if err = db.Ping(); err != nil {
		log.Errorf("Unable to establish connection to db: %v", err)
		return userInfo, err
	}

	rows, err := db.Query("SELECT UserId, MultiSigAddress, VoteBits, VoteBitsVersion FROM Users WHERE MultiSigAddress <> ''")
	if err != nil {
		log.Errorf("Unable to query db: %v", err)
		return userInfo, err
	}

	count := 0
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&Userid, &MultiSigAddress, &VoteBits, &VoteBitsVersion)
		if err != nil {
			log.Errorf("Unable to scan row %v", err)
			continue
		}
		userInfo[MultiSigAddress] = UserVotingConfig{
			Userid:          Userid,
			MultiSigAddress: MultiSigAddress,
			VoteBits:        uint16(VoteBits),
			VoteBitsVersion: uint32(VoteBitsVersion),
		}
		count++
	}

	err = db.Close()
	if err != nil {
		log.Errorf("Unable to close database: %v", err)
		return userInfo, err
	}

	userNoun := pickNoun(count, "user", "users")
	log.Infof("fetch voting config for %d %s", count, userNoun)

	return userInfo, nil
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
	ctx.RLock()
	defer ctx.RUnlock()

	txStrs := make([]string, 0, len(wt.winningTickets))
	for _, ticket := range wt.winningTickets {
		tS := ticket.String()
		txStrs = append(txStrs, tS)
		t, ok := ctx.tickets[tS]
		if !ok {
			log.Debugf("unmanaged winning ticket: %v", tS)
			if ctx.testing {
				panic("boom")
			}
			continue
		}

		log.Infof("winning ticket %v height %v block hash %v msa %v",
			ticket, wt.blockHeight, wt.blockHash, t)

		if !ctx.testing {
			err := ctx.sendTickets(wt.blockHash, ticket, t,
				wt.blockHeight)
			if err != nil {
				log.Infof("%v", err)
			}
		}
	}
	log.Debugf("OnWinningTickets from %v tickets for height %v: %v",
		wt.host, wt.blockHeight, strings.Join(txStrs, ", "))

	ctx.reload <- struct{}{}
}

func (ctx *appContext) reloadHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case <-ctx.reload:
			start := time.Now()
			newTickets := walletFetchUserTickets(ctx)
			end := time.Now()

			// replace tickets
			ctx.Lock()
			ctx.tickets = newTickets
			ctx.Unlock()

			log.Infof("walletFetchUserTickets: %v", end.Sub(start))
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
