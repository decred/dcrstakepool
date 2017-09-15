// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"

	_ "github.com/go-sql-driver/mysql"
)

type appContext struct {
	sync.RWMutex

	// locking required
	liveTicketsMSA          map[chainhash.Hash]string            // [ticket]multisigaddr
	manuallyAddedTicketsMSA map[chainhash.Hash]string            // [ticket]multisigaddr
	userVotingConfig        map[string]userdata.UserVotingConfig // [multisigaddr]

	// no locking required
	coldwalletextpub       *hdkeychain.ExtendedKey
	dataPath               string
	feeAddrs               map[string]struct{}
	feePercent             float64
	newTicketsChan         chan NewTicketsForBlock
	nodeConnection         *dcrrpcclient.Client
	params                 *chaincfg.Params
	wg                     sync.WaitGroup // wait group for go routine exits
	quit                   chan struct{}
	reloadTickets          chan struct{}
	reloadUserConfig       chan struct{}
	spentmissedTicketsChan chan SpentMissedTicketsForBlock
	userData               *userdata.UserData
	votingConfig           *VotingConfig
	walletConnection       *dcrrpcclient.Client
	winningTicketsChan     chan WinningTicketsForBlock
	testing                bool // enabled only for testing
}

type NewTicketsForBlock struct {
	blockHash   *chainhash.Hash
	blockHeight int64
	newTickets  []*chainhash.Hash
}

type SpentMissedTicketsForBlock struct {
	blockHash   *chainhash.Hash
	blockHeight int64
	smTickets   map[*chainhash.Hash]bool
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
	winningTickets []*chainhash.Hash
}

var (
	cfg              *config
	errDuplicateVote = "-32603: already have transaction "
	errNoTxInfo      = "-5: No information for transaction"
	errSuccess       = errors.New("success")

	dataFilenameTemplate = "KIND-DATE-VERSION.gob"
	// save individual versions of fields in case they're changed in the future
	// and keep a global version that represents the overall schema version too
	dataVersionCommon               = "1.0.0"
	dataVersionLiveTickets          = "1.0.0"
	dataVersionManuallyAddedTickets = "1.0.0"
	dataVersionUserVotingConfig     = "1.0.0"
	saveFilesToKeep                 = 10
	saveFileSchema                  = struct {
		LiveTickets          string
		ManuallyAddedTickets string
		UserVotingConfig     string
		Version              string
	}{
		LiveTickets:          dataVersionLiveTickets,
		ManuallyAddedTickets: dataVersionManuallyAddedTickets,
		UserVotingConfig:     dataVersionUserVotingConfig,
		Version:              dataVersionCommon,
	}
	ticketTypeNew         = "New"
	ticketTypeSpentMissed = "SpentMissed"
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

	log.Infof("Version %s (Go version %s)", version(), runtime.Version())
	log.Infof("Network: %s", activeNetParams.Params.Name)
	log.Infof("Home dir: %s", cfg.HomeDir)

	// Create the data directory in case it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		log.Errorf("unable to create data directory: %v", cfg.DataDir)
		return err
	}

	feeAddrs, err := calculateFeeAddresses(cfg.ColdWalletExtPub,
		activeNetParams.Params)
	if err != nil {
		log.Errorf("Error calculating fee payment addresses: %v", err)
		return err
	}

	dcrrpcclient.UseLogger(clientLog)

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

	votingConfig := VotingConfig{
		VoteBits:         walletInfoRes.VoteBits,
		VoteBitsExtended: walletInfoRes.VoteBitsExtended,
		VoteVersion:      walletInfoRes.VoteVersion,
	}
	log.Infof("default voting config: VoteVersion %v VoteBits %v", votingConfig.VoteVersion,
		votingConfig.VoteBits)

	var userdata = &userdata.UserData{}
	userdata.DBSetConfig(cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	userVotingConfig, err := userdata.MySQLFetchUserVotingConfig()
	if err != nil {
		log.Errorf("could not obtain voting config from MySQL: %v", err)
	} else {
		log.Infof("loaded prefs for %d users from MySQL", len(userVotingConfig))
	}

	err = txrules.IsValidPoolFeeRate(cfg.PoolFees)
	if err != nil {
		log.Errorf("poolfees is invalid: %v", err)
		return err
	}

	ctx := &appContext{
		dataPath:               cfg.DataDir,
		feeAddrs:               feeAddrs,
		feePercent:             cfg.PoolFees,
		newTicketsChan:         make(chan NewTicketsForBlock),
		params:                 activeNetParams.Params,
		quit:                   make(chan struct{}),
		reloadUserConfig:       make(chan struct{}),
		reloadTickets:          make(chan struct{}),
		spentmissedTicketsChan: make(chan SpentMissedTicketsForBlock),
		userData:               userdata,
		userVotingConfig:       userVotingConfig,
		votingConfig:           &votingConfig,
		walletConnection:       walletConn,
		winningTicketsChan:     make(chan WinningTicketsForBlock),
		testing:                false,
	}

	// Daemon client connection
	nodeConn, nodeVer, err := connectNodeRPC(ctx, cfg)
	if err != nil || nodeConn == nil {
		log.Infof("Connection to dcrd failed: %v", err)
		return err
	}
	ctx.nodeConnection = nodeConn

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

	// load userVotingConfig from disk cache if necessary
	if len(userVotingConfig) == 0 {
		err = loadData(ctx, "UserVotingConfig")
		if err != nil {
			// we could possibly die out here but it's probably better
			// to let stakepoold vote with default preferences rather than
			// not vote at all
			log.Warnf("unable to load user voting preferences from disk "+
				"cache: %v", err)
		} else {
			log.Infof("Loaded UserVotingConfig for %d users from disk cache",
				len(userVotingConfig))
		}
	}

	// refresh the ticket list and make sure a block didn't come in
	// while we were getting it
	for {
		prevHash, prevHeight, err := nodeConn.GetBestBlock()
		if err != nil {
			log.Errorf("unable to get bestblock from dcrd: %v", err)
			return err
		}
		log.Infof("current block height %v hash %v", prevHeight, prevHash)

		ctx.liveTicketsMSA = walletGetTickets(ctx)

		curHash, curHeight, err := nodeConn.GetBestBlock()
		if err != nil {
			log.Errorf("unable to get bestblock from dcrd: %v", err)
			return err
		}

		// if a block didn't come in while we were processing tickets
		// then we're fine
		if prevHash.IsEqual(curHash) && prevHeight == curHeight {
			break
		}
		log.Infof("block %v came in during GetTickets, refreshing...",
			curHeight)
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
		log.Info("CTRL+C hit.  Closing goroutines.")
		saveData(ctx)
		close(ctx.quit)
	}()

	ctx.wg.Add(4)
	go ctx.newTicketHandler()
	go ctx.reloadUserConfigHandler()
	go ctx.spentmissedTicketHandler()
	go ctx.winningTicketHandler()

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
func pruneData(ctx *appContext) error {
	saveFiles := getDataNames()

	if !fileExists(ctx.dataPath) {
		return fmt.Errorf("datapath %v doesn't exist", ctx.dataPath)
	}

	for dataKind, dataVersion := range saveFiles {
		var filesToPrune []string

		files, err := ioutil.ReadDir(ctx.dataPath)
		if err != nil {
			return err
		}

		for i, file := range files {
			log.Debugf("entry %d => %s", i, file.Name())
			if strings.HasPrefix(file.Name(), strings.ToLower(dataKind)) &&
				strings.Contains(file.Name(), dataVersion) &&
				strings.HasSuffix(file.Name(), ".gob") {
				filesToPrune = append(filesToPrune, filepath.Join(ctx.dataPath, file.Name()))
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
func loadData(ctx *appContext, dataKind string) error {
	dataVersion := ""
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

	if fileExists(ctx.dataPath) {
		files, err := ioutil.ReadDir(ctx.dataPath)
		if err != nil {
			return err
		}

		lastseen := ""

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

		fullPath := filepath.Join(ctx.dataPath, lastseen)

		r, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		dec := gob.NewDecoder(r)
		switch dataKind {
		case "LiveTickets":
			err = dec.Decode(&ctx.liveTicketsMSA)
			if err != nil {
				return err
			}
		case "ManuallyAddedTickets":
			err = dec.Decode(&ctx.manuallyAddedTicketsMSA)
			if err != nil {
				return err
			}
		case "UserVotingConfig":
			err = dec.Decode(&ctx.userVotingConfig)
			if err != nil {
				return err
			}
		}
		log.Infof("Loaded %s from %s", dataKind, fullPath)
		return nil
	}

	// shouldn't get here -- data dir is created on startup
	return errors.New("loadData - path " + ctx.dataPath + " does not exist")
}

// saveData saves some appContext fields to a file so they can be loaded back
// into memory at next run.
func saveData(ctx *appContext) {
	ctx.Lock()
	defer ctx.Unlock()

	saveFiles := getDataNames()

	for filenameprefix, dataversion := range saveFiles {
		t := time.Now()
		destFilename := strings.Replace(dataFilenameTemplate, "KIND", filenameprefix, -1)
		destFilename = strings.Replace(destFilename, "DATE", t.Format("2006_01_02_15_04_05"), -1)
		destFilename = strings.Replace(destFilename, "VERSION", dataversion, -1)
		destPath := strings.ToLower(filepath.Join(ctx.dataPath, destFilename))

		// Pre-validate whether we'll be saving or not.
		switch filenameprefix {
		case "LiveTickets":
			if len(ctx.liveTicketsMSA) == 0 {
				log.Warn("saveData: liveTicketsMSA is empty; skipping save")
				continue
			}
		case "ManuallyAddedTickets":
			if len(ctx.manuallyAddedTicketsMSA) == 0 {
				// we don't expect to do anything with this yet so don't warn
				// log.Warn("saveData: manuallyAddedTicketsMSA is empty; skipping save")
				continue
			}
		case "UserVotingConfig":
			if len(ctx.userVotingConfig) == 0 {
				log.Warn("saveData: UserVotingConfig is empty; skipping save")
				continue
			}
		default:
			log.Warn("saveData: passed unhandled data name " + filenameprefix)
			continue
		}

		w, err := os.Create(destPath)
		defer w.Close()
		if err != nil {
			log.Errorf("Error opening file %s: %v", ctx.dataPath, err)
			continue
		}

		switch filenameprefix {
		case "LiveTickets":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.liveTicketsMSA); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.dataPath, err)
				continue
			}
		case "ManuallyAddedTickets":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.manuallyAddedTicketsMSA); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.dataPath, err)
				continue
			}
		case "UserVotingConfig":
			enc := gob.NewEncoder(w)
			if err := enc.Encode(&ctx.userVotingConfig); err != nil {
				log.Errorf("Failed to encode file %s: %v", ctx.dataPath, err)
				continue
			}
		}

		log.Infof("saveData: successfully saved %v data to %s",
			filenameprefix, destPath)
	}

}

// ticketMetadata contains all the bits and pieces required to vote new tickets,
// to look up new/missed/spent tickets, and to print statistics after usage.
type ticketMetadata struct {
	blockHash        *chainhash.Hash
	blockHeight      int64
	msa              string                    // multisig
	ticket           *chainhash.Hash           // ticket
	spent            bool                      // spent (true) or missed (false)
	config           userdata.UserVotingConfig // voting config
	duration         time.Duration             // overall vote duration
	getDuration      time.Duration             // time to gettransaction
	txid             *chainhash.Hash           // transaction id
	ticketType       string                    // new or spentmissed
	signDuration     time.Duration             // time to generatevote
	sendDuration     time.Duration             // time to sendrawtransaction
	err              error                     // log errors along the way
	voteBits         uint16                    // voteBits
	voteBitsExtended string                    // voteBits extended
}

// getticket pulls the transaction information for a ticket from dcrwallet. This is a go routine!
func (ctx *appContext) getticket(wg *sync.WaitGroup, nt *ticketMetadata) {
	start := time.Now()

	defer func() {
		nt.duration = time.Since(start)
		wg.Done()
	}()

	// Ask wallet to look up vote transaction to see if it belongs to us
	log.Debugf("calling GetTransaction for %v ticket %v",
		strings.ToLower(nt.ticketType), nt.ticket)
	res, err := ctx.walletConnection.GetTransaction(nt.ticket)
	nt.getDuration = time.Since(start)
	if err != nil {
		// suppress "No information for transaction ..." errors
		if !strings.HasPrefix(err.Error(), errNoTxInfo) {
			log.Warnf("unexpected GetTransaction error: '%v' for %v",
				err, nt.ticket)
		}
		return
	}
	for i := range res.Details {
		_, ok := ctx.userVotingConfig[res.Details[i].Address]
		if ok {
			// multisigaddress will match if it belongs a pool user
			nt.msa = res.Details[i].Address

			switch nt.ticketType {
			case ticketTypeNew:
				// Probably doesn't make sense to check manuallyAddedTicketsMSA
				// here since the user/admin won't know about it until later
				log.Debugf("calling nodeCheckTicketFee for %v", nt.ticket)
				ticketFeesValid, err := nodeCheckTicketFee(ctx, res.Hex, res.BlockHash)
				if !ticketFeesValid || err != nil {
					log.Warnf("ignoring ticket %v for msa %v ticketFeesValid %v err %v",
						nt.ticket, nt.msa, ticketFeesValid, err)
					// empty out the multisig address so processNewTickets won't
					// be able to add this ticket
					nt.msa = ""
				}
			}
			break
		}
	}
	log.Debugf("getticket finished for %v ticket %v",
		strings.ToLower(nt.ticketType), nt.ticket)
}

// vote Generates a vote and send it off to the network.  This is a go routine!
func (ctx *appContext) vote(wg *sync.WaitGroup, blockHash *chainhash.Hash, blockHeight int64, w *ticketMetadata) {
	start := time.Now()

	defer func() {
		w.duration = time.Since(start)
		wg.Done()
	}()

	// Ask wallet to generate vote result.
	var res *dcrjson.GenerateVoteResult
	res, w.err = ctx.walletConnection.GenerateVote(blockHash, blockHeight,
		w.ticket, w.config.VoteBits, ctx.votingConfig.VoteBitsExtended)
	if w.err != nil || res.Hex == "" {
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

	// Ask node to transmit raw transaction.
	startSend := time.Now()
	tx, err := ctx.nodeConnection.SendRawTransaction(newTx, false)
	if err != nil {
		log.Infof("vote err %v", err)
		w.err = err
	} else {
		w.txid = tx
	}
	w.sendDuration = time.Since(startSend)
}

func (ctx *appContext) processNewTickets(nt NewTicketsForBlock) {
	start := time.Now()

	// We use pointer because it is the fastest accessor.
	newtickets := make([]*ticketMetadata, 0, len(nt.newTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	ctx.RLock()
	for _, tickethash := range nt.newTickets {
		n := &ticketMetadata{
			blockHash:   nt.blockHash,
			blockHeight: nt.blockHeight,
			ticket:      tickethash,
			ticketType:  ticketTypeNew,
		}
		newtickets = append(newtickets, n)

		wg.Add(1)
		go ctx.getticket(&wg, n)
	}
	ctx.RUnlock()

	wg.Wait()

	addtickets := make(map[chainhash.Hash]string)

	for _, n := range newtickets {
		if n.err != nil || n.msa == "" {
			// most likely can't look up the transaction because it's
			// not in our wallet because it doesn't belong to us
			continue
		}

		addtickets[*n.ticket] = n.msa
	}

	ticketCountNew := 0
	ticketCountOld := 0

	log.Debug("processNewTickets ctx.Lock")
	ctx.Lock()
	ticketCountOld = len(ctx.liveTicketsMSA)
	for ticket, msa := range addtickets {
		ctx.liveTicketsMSA[ticket] = msa
	}
	ticketCountNew = len(ctx.liveTicketsMSA)
	ctx.Unlock()
	log.Debug("processNewTickets ctx.Unlock")

	// Log ticket information outside of the handler.
	go func() {
		for ticket, msa := range addtickets {
			log.Infof("added new ticket %v msa %v", ticket, msa)
		}

		log.Infof("processNewTickets: height %v block %v duration %v "+
			"newtickets %v ticketCountOld %v ticketCountNew %v", nt.blockHeight,
			nt.blockHash, time.Since(start), len(addtickets), ticketCountOld,
			ticketCountNew)
	}()
}

func (ctx *appContext) processSpentMissedTickets(smt SpentMissedTicketsForBlock) {
	start := time.Now()

	// We use pointer because it is the fastest accessor.
	smtickets := make([]*ticketMetadata, 0, len(smt.smTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	ctx.RLock()
	for ticket, spent := range smt.smTickets {
		sm := &ticketMetadata{
			blockHash:   smt.blockHash,
			blockHeight: smt.blockHeight,
			spent:       spent,
			ticket:      ticket,
			ticketType:  ticketTypeSpentMissed,
		}
		smtickets = append(smtickets, sm)

		wg.Add(1)
		go ctx.getticket(&wg, sm)
	}
	ctx.RUnlock()

	wg.Wait()

	var missedtickets []*chainhash.Hash
	var spenttickets []*chainhash.Hash

	for _, sm := range smtickets {
		if sm.err != nil || sm.msa == "" {
			// most likely can't look up the transaction because it's
			// not in our wallet because it doesn't belong to us
			continue
		}

		if !sm.spent {
			missedtickets = append(missedtickets, sm.ticket)
			continue
		}

		spenttickets = append(spenttickets, sm.ticket)
	}

	ticketCountNew := 0
	ticketCountOld := 0

	log.Debug("processSpentMissedTickets ctx.Lock")
	ctx.Lock()
	ticketCountOld = len(ctx.liveTicketsMSA)
	for _, ticket := range missedtickets {
		delete(ctx.liveTicketsMSA, *ticket)
	}
	for _, ticket := range spenttickets {
		delete(ctx.liveTicketsMSA, *ticket)
	}
	ticketCountNew = len(ctx.liveTicketsMSA)
	ctx.Unlock()
	log.Debug("processSpentMissedTickets ctx.Unlock")

	// Log ticket information outside of the handler.
	go func() {
		for _, ticket := range missedtickets {
			log.Infof("removed missed ticket %v", ticket)
		}
		for _, ticket := range spenttickets {
			log.Infof("removed spent ticket %v", ticket)
		}

		log.Infof("processSpentMissedTickets: height %v block %v "+
			"duration %v spenttickets %v missedtickets %v ticketCountOld %v "+
			"ticketCountNew %v", smt.blockHeight, smt.blockHash,
			time.Since(start), len(spenttickets), len(missedtickets),
			ticketCountOld, ticketCountNew)
	}()
}

// processWinningTickets is called every time a new block comes in to handle
// voting.  The function requires ASAP processing for each vote and therefore
// it is not sequential and hard to read.  This is unfortunate but a reality of
// speeding up code.
func (ctx *appContext) processWinningTickets(wt WinningTicketsForBlock) {
	start := time.Now()

	// We use pointer because it is the fastest accessor.
	winners := make([]*ticketMetadata, 0, len(wt.winningTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	ctx.RLock()
	for _, ticket := range wt.winningTickets {
		// Look up multi sig address.
		msa, ok := ctx.liveTicketsMSA[*ticket]
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

		w := &ticketMetadata{
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
		log.Debugf("calling GenerateVote with blockHash %v blockHeight %v "+
			"ticket %v VoteBits %v VoteBitsExtended %v ",
			wt.blockHash, wt.blockHeight, w.ticket, w.config.VoteBits,
			ctx.votingConfig.VoteBitsExtended)
		go ctx.vote(&wg, wt.blockHash, wt.blockHeight, w)
	}
	ctx.RUnlock()

	wg.Wait()

	// Log ticket information outside of the handler.
	go func() {
		var dupeCount, errorCount, votedCount int

		for _, w := range winners {
			if w.err == nil {
				votedCount++
				w.err = errSuccess
			} else {
				// don't count duplicate votes as errors
				if strings.HasPrefix(w.err.Error(), errDuplicateVote) {
					// copy the txid into our metadata struct so it gets printed
					// properly
					voteErrParts := strings.Split(w.err.Error(), errDuplicateVote)
					w.txid, _ = chainhash.NewHashFromStr(voteErrParts[1])
					dupeCount++
				} else {
					errorCount++
				}
			}
			log.Infof("voted ticket %v (hash: %v bits: %v) msa %v duration %v "+
				"(%v + %v): %v", w.ticket, w.txid, w.config.VoteBits, w.msa,
				w.duration, w.signDuration, w.sendDuration, w.err)
		}
		log.Infof("processWinningTickets: height %v block %v "+
			"duration %v newvotes %v duplicatevotes %v errors %v",
			wt.blockHeight, wt.blockHash, time.Since(start), votedCount,
			dupeCount, errorCount)
	}()
}

func (ctx *appContext) reloadUserConfigHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case <-ctx.reloadUserConfig:
			start := time.Now()
			newUserConfig, err :=
				ctx.userData.MySQLFetchUserVotingConfig()
			log.Infof("MySQLFetchUserVotingConfig: %v",
				time.Since(start))

			if err != nil {
				log.Errorf("unable to reload user config due "+
					"to db error: %v", err)
				continue
			}

			// replace UserVotingConfig
			log.Debug("reloadUserConfigHandler ctx.Lock")
			ctx.Lock()
			ctx.userVotingConfig = newUserConfig
			ctx.Unlock()
			log.Debug("reloadUserConfigHandler ctx.Unlock")
		case <-ctx.quit:
			return
		}
	}
}

func (ctx *appContext) newTicketHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case nt := <-ctx.newTicketsChan:
			go ctx.processNewTickets(nt)
		case <-ctx.quit:
			return
		}
	}
}

func (ctx *appContext) spentmissedTicketHandler() {
	defer ctx.wg.Done()

	for {
		select {
		case smt := <-ctx.spentmissedTicketsChan:
			go ctx.processSpentMissedTickets(smt)
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
