// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/rpcclient/v3"
	"github.com/decred/dcrstakepool/backend/stakepoold/rpc/rpcserver"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrwallet/wallet/v2/txrules"
	"github.com/decred/dcrwallet/wallet/v2/udb"

	_ "github.com/go-sql-driver/mysql"
)

const (
	// TODO: sync with controllers/main.go MaxUsers
	numServicePaymentFeeAddresses uint32 = 10000
)

var (
	cfg *config

	dataFilenameTemplate = "KIND-DATE-VERSION.gob"
	// save individual versions of fields in case they're changed in the future
	// and keep a global version that represents the overall schema version too
	dataVersionCommon             = "1.1.0"
	dataVersionAddedLowFeeTickets = "1.0.0"
	dataVersionLiveTickets        = "1.0.0"
	dataVersionUserVotingConfig   = "1.0.0"
	saveFilesToKeep               = 10
	saveFileSchema                = struct {
		AddedLowFeeTickets string
		LiveTickets        string
		UserVotingConfig   string
		Version            string
	}{
		AddedLowFeeTickets: dataVersionAddedLowFeeTickets,
		LiveTickets:        dataVersionLiveTickets,
		UserVotingConfig:   dataVersionUserVotingConfig,
		Version:            dataVersionCommon,
	}
)

// calculateFeeAddresses decodes the string of voting service payment addresses
// to search incoming tickets for. The format for the passed string is:
// "xpub...:end"
// where xpub... is the extended public key and end is the last
// address index to scan to, exclusive. Effectively, it returns the derived
// addresses for this public key for the address indexes [0,end). The branch
// used for the derivation is always the external branch.
func calculateFeeAddresses(xpubStr string, params *chaincfg.Params) (map[string]struct{}, error) {
	end := numServicePaymentFeeAddresses

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

func runMain() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg = loadedCfg

	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	log.Infof("Network: %s", activeNetParams.Params.Name)
	log.Infof("Home dir: %s", cfg.HomeDir)

	// Create the data directory in case it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		log.Errorf("unable to create data directory: %v", cfg.DataDir)
		return err
	}

	log.Infof("Please wait, deriving %v voting service fees addresses "+
		"for extended public key %s", numServicePaymentFeeAddresses,
		cfg.ColdWalletExtPub)
	feeAddrs, err := calculateFeeAddresses(cfg.ColdWalletExtPub,
		activeNetParams.Params)
	if err != nil {
		log.Errorf("Error calculating fee payment addresses: %v", err)
		return err
	}

	rpcclient.UseLogger(clientLog)

	var walletVer semver
	walletConn, walletVer, err := connectWalletRPC(cfg)
	if err != nil || walletConn == nil {
		log.Infof("Connection to dcrwallet failed: %v", err)
		return err
	}
	log.Infof("Connected to dcrwallet (JSON-RPC API v%s)",
		walletVer.String())
	walletInfoRes, err := walletConn.WalletInfo()
	if err != nil || walletInfoRes == nil {
		log.Errorf("Unable to retrieve walletinfo results")
		return err
	}

	// stakepoold must handle voting.
	if walletInfoRes.Voting {
		err := errors.New("dcrwallet config: voting is enabled")
		log.Error(err)
		return err
	}

	votingConfig := rpcserver.VotingConfig{
		VoteBits:         walletInfoRes.VoteBits,
		VoteBitsExtended: walletInfoRes.VoteBitsExtended,
		VoteVersion:      walletInfoRes.VoteVersion,
	}
	log.Infof("default voting config: VoteVersion %v VoteBits %v", votingConfig.VoteVersion,
		votingConfig.VoteBits)

	var userData = &userdata.UserData{}
	userData.DBSetConfig(cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	addedLowFeeTicketsMSA, errMySQLFetchAddedLowFeeTickets := userData.MySQLFetchAddedLowFeeTickets()
	if errMySQLFetchAddedLowFeeTickets != nil {
		log.Errorf("could not obtain low fee tickets from MySQL: %v", err)
	} else {
		log.Infof("loaded low fee tickets for %d users from MySQL", len(addedLowFeeTicketsMSA))
	}

	userVotingConfig, errMySQLFetchUserVotingConfig := userData.MySQLFetchUserVotingConfig()
	if errMySQLFetchUserVotingConfig != nil {
		log.Errorf("could not obtain voting config from MySQL: %v", err)
	} else {
		log.Infof("loaded prefs for %d users from MySQL", len(userVotingConfig))
	}

	if !txrules.ValidPoolFeeRate(cfg.PoolFees) {
		err = fmt.Errorf("poolfees '%v' is invalid", cfg.PoolFees)
		log.Error(err)
		return err
	}

	ctx := &rpcserver.AppContext{
		AddedLowFeeTicketsMSA:  addedLowFeeTicketsMSA,
		DataPath:               cfg.DataDir,
		ColdWalletExtPub:       cfg.ColdWalletExtPub,
		FeeAddrs:               feeAddrs,
		PoolFees:               cfg.PoolFees,
		NewTicketsChan:         make(chan rpcserver.NewTicketsForBlock),
		Params:                 activeNetParams.Params,
		Quit:                   make(chan struct{}),
		SpentmissedTicketsChan: make(chan rpcserver.SpentMissedTicketsForBlock),
		UserData:               userData,
		UserVotingConfig:       userVotingConfig,
		VotingConfig:           &votingConfig,
		WalletConnection:       walletConn,
		WinningTicketsChan:     make(chan rpcserver.WinningTicketsForBlock),
		Testing:                false,
	}

	// Daemon client connection
	nodeConn, nodeVer, err := connectNodeRPC(ctx, cfg)
	if err != nil || nodeConn == nil {
		log.Infof("Connection to dcrd failed: %v", err)
		return err
	}
	ctx.NodeConnection = nodeConn

	// Display connected network
	curnet, err := nodeConn.GetCurrentNet()
	if err != nil {
		log.Errorf("Unable to get current network from dcrd: %v", err)
		return err
	}
	log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v",
		nodeVer.String(), curnet.String())

	// prune save data
	err = pruneData(ctx)
	if err != nil {
		log.Warnf("pruneData error: %v", err)
	}

	// load AddedLowFeeTicketsMSA from disk cache if necessary
	if len(ctx.AddedLowFeeTicketsMSA) == 0 && errMySQLFetchAddedLowFeeTickets != nil {
		err = loadData(ctx, "AddedLowFeeTickets")
		if err != nil {
			// might not have any so continue
			log.Warnf("unable to load added low fee tickets from disk "+
				"cache: %v", err)
		} else {
			log.Infof("Loaded %v AddedLowFeeTickets from disk cache",
				len(ctx.AddedLowFeeTicketsMSA))
		}
	}

	// load userVotingConfig from disk cache if necessary
	if len(ctx.UserVotingConfig) == 0 && errMySQLFetchUserVotingConfig != nil {
		err = loadData(ctx, "UserVotingConfig")
		if err != nil {
			// we could possibly die out here but it's probably better
			// to let stakepoold vote with default preferences rather than
			// not vote at all
			log.Warnf("unable to load user voting preferences from disk "+
				"cache: %v", err)
		} else {
			log.Infof("Loaded UserVotingConfig for %d users from disk cache",
				len(ctx.UserVotingConfig))
		}
	}

	if len(ctx.UserVotingConfig) == 0 {
		log.Warn("0 active users")
	}

	// refresh the ticket list and make sure a block didn't come in
	// while we were getting it
	for {
		curHash, curHeight, err := nodeConn.GetBestBlock()
		if err != nil {
			log.Errorf("unable to get bestblock from dcrd: %v", err)
			return err
		}
		log.Infof("current block height %v hash %v", curHeight, curHash)

		ctx.IgnoredLowFeeTicketsMSA, ctx.LiveTicketsMSA, err = walletGetTickets(ctx)
		if err != nil {
			log.Errorf("unable to get tickets: %v", err)
			return err
		}

		afterHash, afterHeight, err := nodeConn.GetBestBlock()
		if err != nil {
			log.Errorf("unable to get bestblock from dcrd: %v", err)
			return err
		}

		// if a block didn't come in while we were processing tickets
		// then we're fine
		if curHash.IsEqual(afterHash) && curHeight == afterHeight {
			break
		}
		log.Infof("block %v hash %v came in during GetTickets, refreshing...",
			afterHeight, afterHash)
	}

	if err = nodeConn.NotifyBlocks(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for "+
			"block notifications: %s\n", err.Error())
		return err
	}
	if err = nodeConn.NotifyWinningTickets(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for "+
			"winning tickets notifications: %s\n", err.Error())
		return err
	}
	if err = nodeConn.NotifyNewTickets(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for "+
			"new tickets notifications: %s\n", err.Error())
		return err
	}
	if err = nodeConn.NotifySpentAndMissedTickets(); err != nil {
		fmt.Printf("Failed to register daemon RPC client for "+
			"spent/missed tickets notifications: %s\n", err.Error())
		return err
	}
	log.Info("subscribed to notifications from dcrd")

	if !cfg.NoRPCListen {
		if _, err = startGRPCServers(ctx); err != nil {
			fmt.Printf("Failed to start GRPCServers: %s\n", err.Error())
			return err
		}
	}

	// Only accept a single CTRL+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Start waiting for the interrupt signal
	go func() {
		<-c
		signal.Stop(c)
		// Close the channel so multiple goroutines can get the message
		log.Info("CTRL+C hit.  Closing goroutines.")
		saveData(ctx)
		close(ctx.Quit)
	}()

	ctx.Wg.Add(3)
	go ctx.NewTicketHandler()
	go ctx.SpentmissedTicketHandler()
	go ctx.WinningTicketHandler()

	if cfg.NoRPCListen {
		// Start reloading when a ticker fires
		configTicker := time.NewTicker(time.Second * 240)
		go func() {
			for range configTicker.C {
				err := ctx.UpdateTicketDataFromMySQL()
				if err != nil {
					log.Warnf("UpdateTicketDataFromMySQL failed %v:", err)
				}
				err = ctx.UpdateUserDataFromMySQL()
				if err != nil {
					log.Warnf("UpdateUserDataFromMySQL failed %v:", err)
				}
			}
		}()
	}

	// Wait for CTRL+C to signal goroutines to terminate via quit channel.
	ctx.Wg.Wait()

	return nil
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func getDataNames() map[string]string {
	v := reflect.ValueOf(saveFileSchema)

	saveFiles := make(map[string]string)

	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).Name == "Version" {
			continue
		}
		saveFiles[v.Type().Field(i).Name] = v.Field(i).Interface().(string)
	}

	return saveFiles
}

// pruneData prunes any extra save files.
func pruneData(ctx *rpcserver.AppContext) error {
	saveFiles := getDataNames()

	if !fileExists(ctx.DataPath) {
		return fmt.Errorf("datapath %v doesn't exist", ctx.DataPath)
	}

	for dataKind, dataVersion := range saveFiles {
		var filesToPrune []string

		files, err := ioutil.ReadDir(ctx.DataPath)
		if err != nil {
			return err
		}

		for i, file := range files {
			log.Debugf("entry %d => %s", i, file.Name())
			if strings.HasPrefix(file.Name(), strings.ToLower(dataKind)) &&
				strings.Contains(file.Name(), dataVersion) &&
				strings.HasSuffix(file.Name(), ".gob") {
				filesToPrune = append(filesToPrune, filepath.Join(ctx.DataPath, file.Name()))
			}
		}

		if len(filesToPrune) <= saveFilesToKeep {
			continue
		}

		filesToPruneCount := len(filesToPrune) - saveFilesToKeep
		for i, filepath := range filesToPrune {
			err = os.Remove(filepath)
			if err != nil {
				log.Warnf("unable to prune %v: %v", filepath, err)
			} else {
				log.Infof("pruned old data file %v", filepath)
			}
			if filesToPruneCount == i+1 {
				break
			}
		}
	}

	return nil
}

// loadData looks for and attempts to load into memory the most recent save
// file for a passed data kind.
func loadData(ctx *rpcserver.AppContext, dataKind string) error {
	var dataVersion string
	found := false
	saveFiles := getDataNames()

	for filenameprefix, dataversion := range saveFiles {
		if dataKind == filenameprefix {
			dataVersion = dataversion
			found = true
		}
	}

	if !found {
		return errors.New("unhandled data kind of " + dataKind)
	}

	if fileExists(ctx.DataPath) {
		files, err := ioutil.ReadDir(ctx.DataPath)
		if err != nil {
			return err
		}

		var lastseen string

		for i, file := range files {
			log.Debugf("entry %d => %s", i, file.Name())
			if strings.HasPrefix(file.Name(), strings.ToLower(dataKind)) &&
				strings.Contains(file.Name(), dataVersion) &&
				strings.HasSuffix(file.Name(), ".gob") {
				lastseen = file.Name()
			}
		}

		// we could warn/error here but it's not really a problem.
		// maybe the admin deleted the gob files to reset the cache
		// or the cache hasn't been initialized yet.
		if lastseen == "" {
			return nil
		}

		fullPath := filepath.Join(ctx.DataPath, lastseen)

		r, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		dec := gob.NewDecoder(r)
		switch dataKind {
		case "AddedLowFeeTickets":
			err = dec.Decode(&ctx.AddedLowFeeTicketsMSA)
			if err != nil {
				return err
			}
		case "LiveTickets":
			err = dec.Decode(&ctx.LiveTicketsMSA)
			if err != nil {
				return err
			}
		case "UserVotingConfig":
			err = dec.Decode(&ctx.UserVotingConfig)
			if err != nil {
				return err
			}
		}
		log.Infof("Loaded %s from %s", dataKind, fullPath)
		return nil
	}

	// shouldn't get here -- data dir is created on startup
	return errors.New("loadData - path " + ctx.DataPath + " does not exist")
}

// saveData saves some appContext fields to a file so they can be loaded back
// into memory at next run.
func saveData(ctx *rpcserver.AppContext) {
	ctx.Lock()
	defer ctx.Unlock()

	saveFiles := getDataNames()

	for filenameprefix, dataversion := range saveFiles {
		t := time.Now()
		destFilename := strings.Replace(dataFilenameTemplate, "KIND", filenameprefix, -1)
		destFilename = strings.Replace(destFilename, "DATE", t.Format("2006_01_02_15_04_05"), -1)
		destFilename = strings.Replace(destFilename, "VERSION", dataversion, -1)
		destPath := strings.ToLower(filepath.Join(ctx.DataPath, destFilename))

		// Pre-validate whether we'll be saving or not.
		switch filenameprefix {
		case "AddedLowFeeTickets":
			if len(ctx.AddedLowFeeTicketsMSA) == 0 {
				log.Warn("saveData: addedLowFeeTicketsMSA is empty; skipping save")
				continue
			}
		case "LiveTickets":
			if len(ctx.LiveTicketsMSA) == 0 {
				log.Warn("saveData: liveTicketsMSA is empty; skipping save")
				continue
			}
		case "UserVotingConfig":
			if len(ctx.UserVotingConfig) == 0 {
				log.Warn("saveData: UserVotingConfig is empty; skipping save")
				continue
			}
		default:
			log.Warn("saveData: passed unhandled data name " + filenameprefix)
			continue
		}

		w, err := os.Create(destPath)
		if err != nil {
			log.Errorf("Error opening file %s: %v", ctx.DataPath, err)
			continue
		}
		defer w.Close()

		switch filenameprefix {
		case "AddedLowFeeTickets":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.AddedLowFeeTicketsMSA); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.DataPath, err)
				continue
			}
		case "LiveTickets":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.LiveTicketsMSA); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.DataPath, err)
				continue
			}
		case "UserVotingConfig":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.UserVotingConfig); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.DataPath, err)
				continue
			}
		}

		log.Infof("saveData: successfully saved %v data to %s",
			filenameprefix, destPath)
	}
}
