// Copyright (c) 2019 The Decred developers

// Package stakepool holds the Stakepool struct and processes incomming commands
// and notifications.
package stakepool

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	wallettypes "github.com/decred/dcrwallet/rpc/jsonrpc/types"

	"github.com/decred/dcrd/rpcclient/v4"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
)

var (
	errSuccess            = errors.New("success")
	errNoTxInfo           = "-5: no information for transaction"
	errDuplicateVote      = "-32603: already have transaction "
	ticketTypeNew         = "New"
	ticketTypeSpentMissed = "SpentMissed"
)

type Stakepoold struct {
	sync.RWMutex

	// locking required
	AddedLowFeeTicketsMSA   map[chainhash.Hash]string            // [ticket]multisigaddr
	IgnoredLowFeeTicketsMSA map[chainhash.Hash]string            // [ticket]multisigaddr
	LiveTicketsMSA          map[chainhash.Hash]string            // [ticket]multisigaddr
	UserVotingConfig        map[string]userdata.UserVotingConfig // [multisigaddr]

	// no locking required
	DataPath               string
	ColdWalletExtPub       string
	FeeAddrs               map[string]struct{}
	PoolFees               float64
	NewTicketsChan         chan NewTicketsForBlock
	NodeConnection         *rpcclient.Client
	Params                 *chaincfg.Params
	SpentmissedTicketsChan chan SpentMissedTicketsForBlock
	UserData               *userdata.UserData
	VotingConfig           *VotingConfig
	WalletConnection       *Client
	WinningTicketsChan     chan WinningTicketsForBlock
	Testing                bool // enabled only for testing
}

type NewTicketsForBlock struct {
	BlockHash   *chainhash.Hash
	BlockHeight int64
	NewTickets  []*chainhash.Hash
}

type SpentMissedTicketsForBlock struct {
	BlockHash   *chainhash.Hash
	BlockHeight int64
	SmTickets   map[*chainhash.Hash]bool
}

// VotingConfig contains global voting defaults.
type VotingConfig struct {
	VoteBits         uint16
	VoteVersion      uint32
	VoteBitsExtended string
}

type WinningTicketsForBlock struct {
	BlockHash      *chainhash.Hash
	BlockHeight    int64
	WinningTickets []*chainhash.Hash
}

// ticketMetadata contains all the bits and pieces required to vote new tickets,
// to look up new/missed/spent tickets, and to print statistics after usage.
type ticketMetadata struct {
	blockHash    *chainhash.Hash
	blockHeight  int64
	msa          string                    // multisig
	ticket       *chainhash.Hash           // ticket
	spent        bool                      // spent (true) or missed (false)
	config       userdata.UserVotingConfig // voting config
	duration     time.Duration             // overall vote duration
	getDuration  time.Duration             // time to gettransaction
	hex          string                    // hex encoded tx data
	txid         *chainhash.Hash           // transaction id
	ticketType   string                    // new or spentmissed
	signDuration time.Duration             // time to generatevote
	sendDuration time.Duration             // time to sendrawtransaction
	err          error                     // log errors along the way
}

// EvaluateStakePoolTicket evaluates a voting service ticket to see if it's
// acceptable to the voting service. The ticket must pay out to the voting
// service cold wallet, and must have a sufficient fee.
func (spd *Stakepoold) EvaluateStakePoolTicket(tx *wire.MsgTx, blockHeight int32) (bool, error) {
	// Check the first commitment output (txOuts[1])
	// and ensure that the address found there exists
	// in the list of approved addresses. Also ensure
	// that the fee exists and is of the amount
	// requested by the pool.
	commitmentOut := tx.TxOut[1]
	commitAddr, err := stake.AddrFromSStxPkScrCommitment(
		commitmentOut.PkScript, spd.Params)
	if err != nil {
		return false, fmt.Errorf("Failed to parse commit out addr: %s",
			err.Error())
	}

	// Extract the fee from the ticket.
	in := dcrutil.Amount(0)
	for i := range tx.TxOut {
		if i%2 != 0 {
			commitAmt, err := stake.AmountFromSStxPkScrCommitment(
				tx.TxOut[i].PkScript)
			if err != nil {
				return false, fmt.Errorf("Failed to parse commit "+
					"out amt for commit in vout %v: %s", i, err.Error())
			}
			in += commitAmt
		}
	}
	out := dcrutil.Amount(0)
	for i := range tx.TxOut {
		out += dcrutil.Amount(tx.TxOut[i].Value)
	}
	fees := in - out

	_, exists := spd.FeeAddrs[commitAddr.Address()]
	if exists {
		commitAmt, err := stake.AmountFromSStxPkScrCommitment(
			commitmentOut.PkScript)
		if err != nil {
			return false, fmt.Errorf("failed to parse commit "+
				"out amt: %s", err.Error())
		}

		// Calculate the fee required based on the current
		// height and the required amount from the pool.
		feeNeeded := txrules.StakePoolTicketFee(dcrutil.Amount(
			tx.TxOut[0].Value), fees, blockHeight, spd.PoolFees,
			spd.Params)
		if commitAmt < feeNeeded {
			log.Warnf("User %s submitted ticket %v which "+
				"has less fees than are required to use this "+
				"Voting service and is being skipped (required: %v"+
				", found %v)", commitAddr.Address(),
				tx.TxHash(), feeNeeded, commitAmt)

			// Reject the entire transaction if it didn't
			// pay the pool server fees.
			return false, nil
		}
	} else {
		log.Warnf("Unknown pool commitment address %s for ticket %v",
			commitAddr.Address(), tx.TxHash())
		return false, nil
	}

	log.Debugf("Accepted valid voting service ticket %v committing %v in fees",
		tx.TxHash(), tx.TxOut[0].Value)

	return true, nil
}

// MsgTxFromHex returns a wire.MsgTx struct built from the transaction hex string
func MsgTxFromHex(txhex string) (*wire.MsgTx, error) {
	txBytes, err := hex.DecodeString(txhex)
	if err != nil {
		return nil, err
	}
	msgTx := wire.NewMsgTx()
	if err = msgTx.Deserialize(bytes.NewReader(txBytes)); err != nil {
		return nil, err
	}
	return msgTx, nil
}

// getticket pulls the transaction information for a ticket from dcrwallet. This is a go routine!
func (spd *Stakepoold) getticket(wg *sync.WaitGroup, nt *ticketMetadata) {
	start := time.Now()

	defer func() {
		nt.duration = time.Since(start)
		wg.Done()
	}()

	// Ask wallet to look up vote transaction to see if it belongs to us
	log.Debugf("calling GetTransaction for %v ticket %v",
		strings.ToLower(nt.ticketType), nt.ticket)
	res, err := spd.WalletConnection.RPCClient().GetTransaction(nt.ticket)
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
		_, ok := spd.UserVotingConfig[res.Details[i].Address]
		if ok {
			// multisigaddress will match if it belongs a pool user
			nt.msa = res.Details[i].Address

			if nt.ticketType == ticketTypeNew {
				// TODO(maybe): we could check if the ticket was added to the
				// low fee list here but since it was just mined, it should be
				// extremely unlikely to have been added before it was mined.

				// save for fee checking
				nt.hex = res.Hex
			}
			break
		}
	}
	log.Debugf("getticket finished for %v ticket %v",
		strings.ToLower(nt.ticketType), nt.ticket)
}

func (spd *Stakepoold) UpdateTicketData(newAddedLowFeeTicketsMSA map[chainhash.Hash]string) {
	spd.Lock()

	// apply unconditional updates
	for tickethash, msa := range newAddedLowFeeTicketsMSA {
		// remove from ignored list if present
		delete(spd.IgnoredLowFeeTicketsMSA, tickethash)
		// add to live list
		spd.LiveTicketsMSA[tickethash] = msa
	}

	// if something is being deleted from the db by this update then
	// we need to put it back on the ignored list
	for th, m := range spd.AddedLowFeeTicketsMSA {
		_, exists := newAddedLowFeeTicketsMSA[th]
		if !exists {
			spd.IgnoredLowFeeTicketsMSA[th] = m
		}
	}

	spd.AddedLowFeeTicketsMSA = newAddedLowFeeTicketsMSA
	addedLowFeeTicketsCount := len(spd.AddedLowFeeTicketsMSA)
	ignoredLowFeeTicketsCount := len(spd.IgnoredLowFeeTicketsMSA)
	liveTicketsCount := len(spd.LiveTicketsMSA)
	spd.Unlock()
	// Log ticket information outside of the handler.
	go func() {
		log.Infof("tickets loaded -- addedLowFee %v ignoredLowFee %v live %v "+
			"total %v", addedLowFeeTicketsCount, ignoredLowFeeTicketsCount,
			liveTicketsCount,
			addedLowFeeTicketsCount+ignoredLowFeeTicketsCount+liveTicketsCount)
	}()
}

func (spd *Stakepoold) UpdateTicketDataFromMySQL() error {
	start := time.Now()
	newAddedLowFeeTicketsMSA, err := spd.UserData.MySQLFetchAddedLowFeeTickets()
	log.Infof("MySQLFetchAddedLowFeeTickets took %v", time.Since(start))
	if err != nil {
		return err
	}
	spd.UpdateTicketData(newAddedLowFeeTicketsMSA)
	return nil
}

// ImportNewScript will import a redeem script into dcrwallet. No rescan is
// performed because we are importing a brand new script, it shouldn't have any
// associated history. Current block height is returned to indicate which height
// the new user has registered.
func (spd *Stakepoold) ImportNewScript(script []byte) (int64, error) {
	err := spd.WalletConnection.RPCClient().ImportScriptRescanFrom(script, false, 0)
	if err != nil {
		log.Errorf("ImportNewScript: ImportScriptRescanFrom rpc failed: %v", err)
		return -1, err
	}

	_, bestBlockHeight, err := spd.WalletConnection.RPCClient().GetBestBlock()
	if err != nil {
		log.Errorf("ImportNewScript: GetBestBlock rpc failed: %v", err)
		return -1, err
	}
	return bestBlockHeight, nil
}

// ImportMissingScripts accepts a list of redeem scripts and a rescan height. It
// will import all but one of the scripts without triggering a wallet rescan,
// and finally trigger a rescan from the provided height after importing the
// last one.
func (spd *Stakepoold) ImportMissingScripts(scripts [][]byte, rescanHeight int) error {
	// Import n-1 scripts without a rescan.
	allButOne := scripts[:len(scripts)-1]
	for _, script := range allButOne {
		err := spd.WalletConnection.RPCClient().ImportScriptRescanFrom(script, false, 0)
		if err != nil {
			log.Errorf("ImportMissingScripts: ImportScript rpc failed: %v", err)
			return err
		}
	}

	// Import the last script and trigger a rescan
	lastOne := scripts[len(scripts)-1]
	err := spd.WalletConnection.RPCClient().ImportScriptRescanFrom(lastOne, true, rescanHeight)
	if err != nil {
		log.Errorf("ImportMissingScripts: ImportScriptRescanFrom rpc failed: %v", err)
		return err
	}

	log.Infof("ImportMissingScripts: Imported %d scripts and triggered a rescan from height %d", len(scripts), rescanHeight)

	return nil
}

func (spd *Stakepoold) AddMissingTicket(ticketHash []byte) error {
	log.Infof("AddMissingTicket: Adding ticket with hash %s", ticketHash)

	hash, err := chainhash.NewHash(ticketHash)
	if err != nil {
		log.Errorf("AddMissingTicket: Failed to parse ticket hash: %v", err)
		return err
	}

	tx, err := spd.WalletConnection.RPCClient().GetRawTransaction(hash)
	if err != nil {
		log.Errorf("AddMissingTicket: GetRawTransaction rpc failed: %v", err)
		return err
	}

	err = spd.WalletConnection.RPCClient().AddTicket(tx)
	if err != nil {
		log.Errorf("AddMissingTicket: AddTicket rpc failed: %v", err)
		return err
	}

	return nil
}

func (spd *Stakepoold) ListScripts() ([][]byte, error) {
	scripts, err := spd.WalletConnection.RPCClient().ListScripts()
	if err != nil {
		log.Errorf("ListScripts: ListScripts rpc failed: %v", err)
		return nil, err
	}

	return scripts, nil
}

// CreateMultisig decodes the provided array of addresses, and then
// passes them to dcrwallet to create a 1-of-N multisig address.
func (spd *Stakepoold) CreateMultisig(addresses []string) (*wallettypes.CreateMultiSigResult, error) {
	decodedAddresses := make([]dcrutil.Address, len(addresses))

	for i, addr := range addresses {
		decodedAddress, err := dcrutil.DecodeAddress(addr, spd.Params)
		if err != nil {
			log.Errorf("CreateMultisig: Address could not be decoded %v: %v", addr, err)
			return nil, err
		}
		decodedAddresses[i] = decodedAddress
	}

	result, err := spd.WalletConnection.RPCClient().CreateMultisig(1, decodedAddresses)
	if err != nil {
		log.Errorf("CreateMultisig: CreateMultisig rpc failed: %v", err)
		return nil, err
	}

	return result, nil
}

func (spd *Stakepoold) AccountSyncAddressIndex(account string, branch uint32, index int) error {
	err := spd.WalletConnection.RPCClient().AccountSyncAddressIndex(account, branch, index)
	if err != nil {
		log.Errorf("AccountSyncAddressIndex: AccountSyncAddressIndex rpc failed: %v", err)
		return err
	}

	return nil
}

func (spd *Stakepoold) GetTickets(includeImmature bool) ([]*chainhash.Hash, error) {
	tickets, err := spd.WalletConnection.RPCClient().GetTickets(includeImmature)
	if err != nil {
		log.Errorf("GetTickets: GetTickets rpc failed: %v", err)
		return nil, err
	}

	return tickets, nil
}

func (spd *Stakepoold) StakePoolUserInfo(multisigAddress string) (*wallettypes.StakePoolUserInfoResult, error) {
	decodedMultisig, err := dcrutil.DecodeAddress(multisigAddress, spd.Params)
	if err != nil {
		log.Errorf("StakePoolUserInfo: Address could not be decoded %v: %v", multisigAddress, err)
		return nil, err
	}

	response, err := spd.WalletConnection.RPCClient().StakePoolUserInfo(decodedMultisig)
	if err != nil {
		log.Errorf("StakePoolUserInfo: StakePoolUserInfo rpc failed: %v", err)
		return nil, err
	}

	return response, nil
}

func (spd *Stakepoold) WalletInfo() (*wallettypes.WalletInfoResult, error) {
	response, err := spd.WalletConnection.RPCClient().WalletInfo()
	if err != nil {
		log.Errorf("WalletInfo: WalletInfo rpc failed: %v", err)
		return nil, err
	}

	return response, nil
}

func (spd *Stakepoold) ValidateAddress(address string) (*wallettypes.ValidateAddressWalletResult, error) {
	addr, err := dcrutil.DecodeAddress(address, spd.Params)
	if err != nil {
		log.Errorf("ValidateAddress: ValidateAddress rpc failed: %v", err)
		return nil, err
	}

	response, err := spd.WalletConnection.RPCClient().ValidateAddress(addr)
	if err != nil {
		log.Errorf("ValidateAddress: ValidateAddress rpc failed: %v", err)
		return nil, err
	}

	return response, nil
}

// GetStakeInfo performs the rpc command GetStakeInfo.
func (spd *Stakepoold) GetStakeInfo() (*wallettypes.GetStakeInfoResult, error) {
	response, err := spd.WalletConnection.RPCClient().GetStakeInfo()
	if err != nil {
		log.Errorf("GetStakeInfo: GetStakeInfo rpc failed: %v", err)
		return nil, err
	}

	return response, nil
}

func (spd *Stakepoold) UpdateUserData(newUserVotingConfig map[string]userdata.UserVotingConfig) {
	spd.Lock()
	spd.UserVotingConfig = newUserVotingConfig
	spd.Unlock()
}

func (spd *Stakepoold) UpdateUserDataFromMySQL() error {
	start := time.Now()
	newUserVotingConfig, err := spd.UserData.MySQLFetchUserVotingConfig()
	log.Infof("MySQLFetchUserVotingConfig took %v",
		time.Since(start))
	if err != nil {
		return err
	}
	spd.UpdateUserData(newUserVotingConfig)
	return nil
}

// vote Generates a vote and send it off to the network.  This is a go routine!
func (spd *Stakepoold) vote(wg *sync.WaitGroup, blockHash *chainhash.Hash, blockHeight int64, w *ticketMetadata) {
	start := time.Now()

	defer func() {
		w.duration = time.Since(start)
		wg.Done()
	}()

	// Ask wallet to generate vote result.
	var res *wallettypes.GenerateVoteResult
	res, w.err = spd.WalletConnection.RPCClient().GenerateVote(blockHash, blockHeight,
		w.ticket, w.config.VoteBits, spd.VotingConfig.VoteBitsExtended)
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
	tx, err := spd.NodeConnection.SendRawTransaction(newTx, false)
	if err != nil {
		log.Infof("vote err %v", err)
		w.err = err
	} else {
		w.txid = tx
	}
	w.sendDuration = time.Since(startSend)
}

// processNewTickets is invoked every time a new block is created. Any tickets
// which matured in this block will be included in the parameter.
func (spd *Stakepoold) processNewTickets(nt NewTicketsForBlock) {
	start := time.Now()

	log.Debugf("processNewTickets: Block %d contains %d newly matured tickets",
		nt.BlockHeight, len(nt.NewTickets))

	// We use pointer because it is the fastest accessor.
	newtickets := make([]*ticketMetadata, 0, len(nt.NewTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	spd.RLock()
	for _, tickethash := range nt.NewTickets {
		n := &ticketMetadata{
			blockHash:   nt.BlockHash,
			blockHeight: nt.BlockHeight,
			ticket:      tickethash,
			ticketType:  ticketTypeNew,
		}
		newtickets = append(newtickets, n)

		wg.Add(1)
		go spd.getticket(&wg, n)
	}
	spd.RUnlock()

	wg.Wait()

	newIgnoredLowFeeTickets := make(map[chainhash.Hash]string)
	newLiveTickets := make(map[chainhash.Hash]string)

	for _, n := range newtickets {
		if n.err != nil || n.msa == "" {
			// most likely can't look up the transaction because it's
			// not in our wallet because it doesn't belong to us
			continue
		}

		msgTx, err := MsgTxFromHex(n.hex)
		if err != nil {
			log.Warnf("processNewTickets: MsgTxFromHex failed for %v: %v", n.hex, err)
			continue
		}

		ticketFeesValid, err := spd.EvaluateStakePoolTicket(msgTx, int32(nt.BlockHeight))

		if err != nil {
			log.Warnf("ignoring ticket %v for multisig %v due to error: %v", n.ticket, n.msa, err)
			newIgnoredLowFeeTickets[*n.ticket] = n.msa
		} else if ticketFeesValid {
			newLiveTickets[*n.ticket] = n.msa
		} else {
			log.Warnf("ignoring ticket %v for multisig %v due to invalid fee", n.ticket, n.msa)
			newIgnoredLowFeeTickets[*n.ticket] = n.msa
		}
	}

	spd.Lock()
	// update ignored low fee tickets
	for ticket, msa := range newIgnoredLowFeeTickets {
		spd.IgnoredLowFeeTicketsMSA[ticket] = msa
	}

	// update live tickets
	for ticket, msa := range newLiveTickets {
		spd.LiveTicketsMSA[ticket] = msa
	}

	// update counts
	addedLowFeeTicketsCount := len(spd.AddedLowFeeTicketsMSA)
	ignoredLowFeeTicketsCount := len(spd.IgnoredLowFeeTicketsMSA)
	liveTicketsCount := len(spd.LiveTicketsMSA)
	spd.Unlock()

	// Log ticket information outside of the handler.
	go func() {
		for ticket, msa := range newLiveTickets {
			log.Infof("processNewTickets: added new live ticket %v multisig %v", ticket, msa)
		}

		for ticket, msa := range newIgnoredLowFeeTickets {
			log.Infof("processNewTickets: added new ignored ticket %v multisig %v", ticket, msa)
		}

		log.Infof("processNewTickets: height %v block %v duration %v "+
			"ignored %v live %v notours %v", nt.BlockHeight,
			nt.BlockHash, time.Since(start), len(newIgnoredLowFeeTickets),
			len(newLiveTickets),
			len(nt.NewTickets)-len(newIgnoredLowFeeTickets)-len(newLiveTickets))
		log.Infof("processNewTickets: tickets loaded -- addedLowFee %v ignoredLowFee %v live %v "+
			"total %v", addedLowFeeTicketsCount, ignoredLowFeeTicketsCount,
			liveTicketsCount,
			addedLowFeeTicketsCount+ignoredLowFeeTicketsCount+liveTicketsCount)
	}()
}

// processSpentMissedTickets is invoked every time a new block is created. Any
// tickets which are either spent or missed in this block will be included in
// the parameter.
func (spd *Stakepoold) processSpentMissedTickets(smt SpentMissedTicketsForBlock) {
	start := time.Now()

	// We use pointer because it is the fastest accessor.
	smtickets := make([]*ticketMetadata, 0, len(smt.SmTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	spd.RLock()
	for ticket, spent := range smt.SmTickets {
		sm := &ticketMetadata{
			blockHash:   smt.BlockHash,
			blockHeight: smt.BlockHeight,
			spent:       spent,
			ticket:      ticket,
			ticketType:  ticketTypeSpentMissed,
		}
		smtickets = append(smtickets, sm)

		wg.Add(1)
		go spd.getticket(&wg, sm)
	}
	spd.RUnlock()

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

	var ticketCountNew int
	var ticketCountOld int

	spd.Lock()
	ticketCountOld = len(spd.LiveTicketsMSA)
	for _, ticket := range missedtickets {
		delete(spd.IgnoredLowFeeTicketsMSA, *ticket)
		delete(spd.LiveTicketsMSA, *ticket)
	}
	for _, ticket := range spenttickets {
		delete(spd.IgnoredLowFeeTicketsMSA, *ticket)
		delete(spd.LiveTicketsMSA, *ticket)
	}
	ticketCountNew = len(spd.LiveTicketsMSA)
	spd.Unlock()

	// Log ticket information outside of the handler.
	go func() {
		for _, ticket := range missedtickets {
			log.Infof("processSpentMissedTickets: removed missed ticket %v", ticket)
		}
		for _, ticket := range spenttickets {
			log.Infof("processSpentMissedTickets: removed spent ticket %v", ticket)
		}

		log.Infof("processSpentMissedTickets: height %v block %v "+
			"duration %v spenttickets %v missedtickets %v ticketCountOld %v "+
			"ticketCountNew %v", smt.BlockHeight, smt.BlockHash,
			time.Since(start), len(spenttickets), len(missedtickets),
			ticketCountOld, ticketCountNew)
	}()
}

// ProcessWinningTickets is called every time a new block comes in to handle
// voting.  The function requires ASAP processing for each vote and therefore
// it is not sequential and hard to read.  This is unfortunate but a reality of
// speeding up code.
func (spd *Stakepoold) ProcessWinningTickets(wt WinningTicketsForBlock) {
	start := time.Now()

	log.Debugf("ProcessWinningTickets: Block %d contains %d winning tickets", wt.BlockHeight, len(wt.WinningTickets))

	// We use pointer because it is the fastest accessor.
	winners := make([]*ticketMetadata, 0, len(wt.WinningTickets))

	var wg sync.WaitGroup // wait group for go routine exits

	spd.RLock()
	for _, ticket := range wt.WinningTickets {
		// Look up multi sig address.
		msa, ok := spd.LiveTicketsMSA[*ticket]
		if !ok {
			log.Debugf("ProcessWinningTickets: unmanaged winning ticket: %v", ticket)
			if spd.Testing {
				panic("boom")
			}
			continue
		}

		voteCfg, ok := spd.UserVotingConfig[msa]
		if !ok {
			// Use defaults if not found.
			log.Warnf("ProcessWinningTickets: vote config not found for %v using defaults",
				msa)
			voteCfg = userdata.UserVotingConfig{
				Userid:          0,
				MultiSigAddress: msa,
				VoteBits:        spd.VotingConfig.VoteBits,
				VoteBitsVersion: spd.VotingConfig.VoteVersion,
			}
		} else if voteCfg.VoteBitsVersion != spd.VotingConfig.VoteVersion {
			// If the user's voting config has a vote version that
			// is different from our global vote version that we
			// plucked from dcrwallet walletinfo then just use the
			// default votebits.
			voteCfg.VoteBits = spd.VotingConfig.VoteBits
			log.Infof("ProcessWinningTickets: userid %v multisigaddress %v vote "+
				"version mismatch user %v stakepoold "+
				"%v using votebits %d",
				voteCfg.Userid, voteCfg.MultiSigAddress,
				voteCfg.VoteBitsVersion,
				spd.VotingConfig.VoteVersion,
				voteCfg.VoteBits)
		}

		w := &ticketMetadata{
			msa:    msa,
			ticket: ticket,
			config: voteCfg,
		}
		winners = append(winners, w)

		// When testing we don't send the tickets.
		if spd.Testing {
			continue
		}

		wg.Add(1)
		log.Debugf("ProcessWinningTickets: calling GenerateVote with blockHash %v blockHeight %v "+
			"ticket %v VoteBits %v VoteBitsExtended %v ",
			wt.BlockHash, wt.BlockHeight, w.ticket, w.config.VoteBits,
			spd.VotingConfig.VoteBitsExtended)
		go spd.vote(&wg, wt.BlockHash, wt.BlockHeight, w)
	}
	spd.RUnlock()

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
			log.Infof("ProcessWinningTickets: voted ticket %v (hash: %v bits: %v) multisig %v duration %v "+
				"(%v + %v): %v", w.ticket, w.txid, w.config.VoteBits, w.msa,
				w.duration, w.signDuration, w.sendDuration, w.err)
		}
		log.Infof("ProcessWinningTickets: height %v block %v "+
			"duration %v newvotes %v duplicatevotes %v errors %v",
			wt.BlockHeight, wt.BlockHash, time.Since(start), votedCount,
			dupeCount, errorCount)
	}()
}

func (spd *Stakepoold) NewTicketHandler(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case nt := <-spd.NewTicketsChan:
			go spd.processNewTickets(nt)
		case <-ctx.Done():
			return
		}
	}
}

func (spd *Stakepoold) SpentmissedTicketHandler(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case smt := <-spd.SpentmissedTicketsChan:
			go spd.processSpentMissedTickets(smt)
		case <-ctx.Done():
			return
		}
	}
}

func (spd *Stakepoold) WinningTicketHandler(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case wt := <-spd.WinningTicketsChan:
			go spd.ProcessWinningTickets(wt)
		case <-ctx.Done():
			return
		}
	}
}
