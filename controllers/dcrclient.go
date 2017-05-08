// dcrclient.go

package controllers

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/wallet/udb"
)

// functionName
type functionName int

const (
	validateAddressFn functionName = iota
	createMultisigFn
	importScriptFn
	ticketsForAddressFn
	getTxOutFn
	getStakeInfoFn
	connectedFn
	stakePoolUserInfoFn
	getBestBlockFn
)

var (
	// cacheTimerStakeInfo is the duration of time after which to
	// access the wallet and update the stake information instead
	// of returning cached stake information.
	cacheTimerStakeInfo = 5 * time.Minute

	// cacheTimerGetTickets is the duration of time after which to
	// access the wallet and update the ticket list for an address
	// instead of returning cached ticket information.
	cacheTimerGetTickets = 20 * time.Second

	// defaultAccountName is the account name for the default wallet
	// account as a string.
	defaultAccountName = "default"
)

// validateAddressResponse
type validateAddressResponse struct {
	addrInfo *dcrjson.ValidateAddressWalletResult
	err      error
}

// validateAddressMsg
type validateAddressMsg struct {
	address dcrutil.Address
	reply   chan validateAddressResponse
}

// createMultisigResponse
type createMultisigResponse struct {
	multisigInfo *dcrjson.CreateMultiSigResult
	err          error
}

// createMultisigMsg
type createMultisigMsg struct {
	required  int
	addresses []dcrutil.Address
	reply     chan createMultisigResponse
}

// importScriptResponse
type importScriptResponse struct {
	err error
}

// importScriptMsg
type importScriptMsg struct {
	height int
	script []byte
	reply  chan importScriptResponse
}

// ticketsForAddressResponse
type ticketsForAddressResponse struct {
	tickets *dcrjson.TicketsForAddressResult
	err     error
}

// ticketsForAddressMsg
type ticketsForAddressMsg struct {
	address dcrutil.Address
	reply   chan ticketsForAddressResponse
}

// getTxOutResponse
type getTxOutResponse struct {
	txOut *dcrjson.GetTxOutResult
	err   error
}

// getTxOutMsg
type getTxOutMsg struct {
	hash  *chainhash.Hash
	idx   uint32
	reply chan getTxOutResponse
}

// getStakeInfoResponse
type getStakeInfoResponse struct {
	stakeInfo *dcrjson.GetStakeInfoResult
	err       error
}

// getStakeInfoMsg
type getStakeInfoMsg struct {
	reply chan getStakeInfoResponse
}

// connectedResponse
type connectedResponse struct {
	walletInfo []*dcrjson.WalletInfoResult
	err        error
}

// connectedMsg
type connectedMsg struct {
	reply chan connectedResponse
}

// stakePoolUserInfoResponse
type stakePoolUserInfoResponse struct {
	userInfo *dcrjson.StakePoolUserInfoResult
	err      error
}

// stakePoolUserInfoMsg
type stakePoolUserInfoMsg struct {
	userAddr dcrutil.Address
	reply    chan stakePoolUserInfoResponse
}

// getBestBlockResponse
type getBestBlockResponse struct {
	bestBlockHash   *chainhash.Hash
	bestBlockHeight int64
	err             error
}

// getBestBlockMsg
type getBestBlockMsg struct {
	reply chan getBestBlockResponse
}

// connectionError is an error relating to the connection,
// so that connection failures can be handled without
// crashing the server.
type connectionError error

// walletRPCHandler
func (w *walletSvrManager) walletRPCHandler() {
out:
	for {
		select {
		case m := <-w.msgChan:
			switch msg := m.(type) {
			case validateAddressMsg:
				resp := w.executeInSequence(validateAddressFn, msg)
				respTyped := resp.(*validateAddressResponse)
				msg.reply <- *respTyped
			case createMultisigMsg:
				resp := w.executeInSequence(createMultisigFn, msg)
				respTyped := resp.(*createMultisigResponse)
				msg.reply <- *respTyped
			case importScriptMsg:
				resp := w.executeInSequence(importScriptFn, msg)
				respTyped := resp.(*importScriptResponse)
				msg.reply <- *respTyped
			case ticketsForAddressMsg:
				resp := w.executeInSequence(ticketsForAddressFn, msg)
				respTyped := resp.(*ticketsForAddressResponse)
				msg.reply <- *respTyped
			case getTxOutMsg:
				resp := w.executeInSequence(getTxOutFn, msg)
				respTyped := resp.(*getTxOutResponse)
				msg.reply <- *respTyped
			case getStakeInfoMsg:
				resp := w.executeInSequence(getStakeInfoFn, msg)
				respTyped := resp.(*getStakeInfoResponse)
				msg.reply <- *respTyped
			case connectedMsg:
				resp := w.executeInSequence(connectedFn, msg)
				respTyped := resp.(*connectedResponse)
				msg.reply <- *respTyped
			case stakePoolUserInfoMsg:
				resp := w.executeInSequence(stakePoolUserInfoFn, msg)
				respTyped := resp.(*stakePoolUserInfoResponse)
				msg.reply <- *respTyped
			case getBestBlockMsg:
				resp := w.executeInSequence(getBestBlockFn, msg)
				respTyped := resp.(*getBestBlockResponse)
				msg.reply <- *respTyped
			default:
				log.Infof("Invalid message type in wallet RPC "+
					"handler: %T", msg)
			}

		case <-w.quit:
			break out
		}
	}

	w.wg.Done()
	log.Infof("Wallet RPC handler done")
}

// executeInSequence is the mainhandler of all the incoming client functions.
func (w *walletSvrManager) executeInSequence(fn functionName, msg interface{}) interface{} {
	switch fn {
	case validateAddressFn:
		vam := msg.(validateAddressMsg)
		resp := new(validateAddressResponse)
		vawrs := make([]*dcrjson.ValidateAddressWalletResult, w.serversLen)
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			vawr, err := s.ValidateAddress(vam.address)
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("validateAddressFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				vawrs[i] = nil
				continue
			}
			connectCount++
			vawrs[i] = vawr
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for validateAddressFn")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := 0; i < w.serversLen; i++ {
			if i == w.serversLen-1 {
				break
			}
			if vawrs[i] == nil || vawrs[i+1] == nil {
				continue
			}
			if vawrs[i].PubKey != vawrs[i+1].PubKey {
				log.Infof("validateAddressFn nonequiv failure on servers "+
					"%v, %v (%v != %v)", i, i+1, vawrs[i].PubKey, vawrs[i+1].PubKey)
				resp.err = fmt.Errorf("non equivalent pubkey returned")
				return resp
			}
		}

		for i := range vawrs {
			if vawrs[i] != nil {
				resp.addrInfo = vawrs[i]
				break
			}
		}
		return resp

	case createMultisigFn:
		cmsm := msg.(createMultisigMsg)
		resp := new(createMultisigResponse)
		cmsrs := make([]*dcrjson.CreateMultiSigResult, w.serversLen)
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			cmsr, err := s.CreateMultisig(cmsm.required, cmsm.addresses)
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("createMultisigFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				cmsrs[i] = nil
				continue
			}
			connectCount++
			cmsrs[i] = cmsr
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for createMultisigFn")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := 0; i < w.serversLen; i++ {
			if i == w.serversLen-1 {
				break
			}
			if cmsrs[i] == nil || cmsrs[i+1] == nil {
				continue
			}
			if cmsrs[i].RedeemScript != cmsrs[i+1].RedeemScript {
				log.Infof("createMultisigFn nonequiv failure on servers "+
					"%v, %v (%v != %v)", i, i+1, cmsrs[i].RedeemScript, cmsrs[i+1].RedeemScript)
				resp.err = fmt.Errorf("non equivalent redeem script returned")
				return resp
			}
		}

		for i := range cmsrs {
			if cmsrs[i] != nil {
				resp.multisigInfo = cmsrs[i]
				break
			}
		}
		return resp

	case importScriptFn:
		ism := msg.(importScriptMsg)
		resp := new(importScriptResponse)
		isErrors := make([]error, w.serversLen)
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			err := s.ImportScriptRescanFrom(ism.script, true, ism.height)
			isErrors[i] = err
		}

		for i := 0; i < w.serversLen; i++ {
			if i == w.serversLen-1 {
				break
			}

			notIsNil1 := isErrors[i] != nil
			notIsNil2 := isErrors[i+1] != nil
			if notIsNil1 != notIsNil2 {
				log.Infof("importScriptFn nonequiv failure 1 on servers %v, %v",
					i, i+1)
				resp.err = fmt.Errorf("non equivalent error returned 1")
				return resp
			}

			if notIsNil1 && notIsNil2 {
				if isErrors[i].Error() != isErrors[i+1].Error() {
					log.Infof("importScriptFn nonequiv failure 2 on  "+
						"servers %v, %v", i, i+1)
					resp.err = fmt.Errorf("non equivalent error returned 2")
					return resp
				}
			}
		}

		resp.err = isErrors[0]
		return resp

	case ticketsForAddressFn:
		tfam := msg.(ticketsForAddressMsg)
		resp := new(ticketsForAddressResponse)
		tfars := make([]*dcrjson.TicketsForAddressResult, w.serversLen)
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			// Returns all tickets - even unconfirmed/mempool - when wallet is
			// queried
			tfar, err := s.TicketsForAddress(tfam.address)
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("ticketsForAddressFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				tfars[i] = nil
				continue
			}
			connectCount++
			tfars[i] = tfar
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for stakepooluserinfo")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := range tfars {
			if tfars[i] != nil {
				resp.tickets = tfars[i]
				break
			}
		}
		return resp

	case getTxOutFn:
		gtom := msg.(getTxOutMsg)
		resp := new(getTxOutResponse)
		gtors := make([]*dcrjson.GetTxOutResult, w.serversLen)
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			gtor, err := s.GetTxOut(gtom.hash, gtom.idx, true)
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("getTxOutFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				gtors[i] = nil
				continue
			}
			connectCount++
			gtors[i] = gtor
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for getTxOutFn")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := 0; i < w.serversLen; i++ {
			if i == w.serversLen-1 {
				break
			}
			if gtors[i] == nil || gtors[i+1] == nil {
				continue
			}
			if gtors[i].ScriptPubKey.Hex != gtors[i+1].ScriptPubKey.Hex {
				log.Infof("getTxOutFn nonequiv failure on servers "+
					"%v, %v", i, i+1)
				resp.err = fmt.Errorf("non equivalent ScriptPubKey returned")
				return resp
			}
		}

		for i := range gtors {
			if gtors[i] != nil {
				resp.txOut = gtors[i]
				break
			}
		}
		return resp

	case getStakeInfoFn:
		resp := new(getStakeInfoResponse)
		gsirs := make([]*dcrjson.GetStakeInfoResult, w.serversLen)
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			gsir, err := s.GetStakeInfo()
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("getStakeInfoFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				gsirs[i] = nil
				continue
			}
			connectCount++
			gsirs[i] = gsir
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for getStakeInfoFn")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := 0; i < w.serversLen; i++ {
			if i == w.serversLen-1 {
				break
			}
			if gsirs[i] == nil || gsirs[i+1] == nil {
				continue
			}
			if gsirs[i].Live != gsirs[i+1].Live {
				log.Infof("getStakeInfoFn nonequiv failure on servers "+
					"%v, %v", i, i+1)
				resp.err = fmt.Errorf("non equivalent Live returned")
				return resp
			}
		}

		for i := range gsirs {
			if gsirs[i] != nil {
				resp.stakeInfo = gsirs[i]
				break
			}
		}
		return resp

	// connectedFn actually requests walletinfo from the wallet and makes
	// sure the daemon is connected and the wallet is unlocked.
	case connectedFn:
		resp := new(connectedResponse)
		resp.err = nil
		wirs := make([]*dcrjson.WalletInfoResult, w.serversLen)
		resp.walletInfo = wirs
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			wir, err := s.WalletInfo()
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("connectedFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				wirs[i] = nil
				continue
			}
			connectCount++
			wirs[i] = wir
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for walletinfo")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		for i := 0; i < w.serversLen; i++ {
			if wirs[i] == nil {
				continue
			}
			// Check to make sure we're connected to the daemon.
			// If we aren't, send a failure.
			if !wirs[i].DaemonConnected {
				log.Infof("daemon connectivity failure on svr %v", i)
				return fmt.Errorf("wallet server %v not connected to daemon", i)
			}

			// Check to make sure the wallet is unlocked.
			if !wirs[i].Unlocked {
				log.Infof("wallet svr %v not unlocked", i)
				return fmt.Errorf("wallet server %v locked", i)
			}
		}

		// TODO add infrastructure to decide if a certain number of
		// wallets up/down is acceptable in the eyes of the admin.
		// For example, allow for RPC calls to wallet if 2/3 are up,
		// but err out and disallow if only 1/3 etc
		return resp

	case stakePoolUserInfoFn:
		spuim := msg.(stakePoolUserInfoMsg)
		resp := new(stakePoolUserInfoResponse)
		spuirs := make([]*dcrjson.StakePoolUserInfoResult, w.serversLen)
		// use connectCount to increment total number of successful responses
		// if we have > 0 then we proceed as though nothing is wrong for the user
		connectCount := 0
		for i, s := range w.servers {
			if w.servers[i] == nil {
				spuirs[i] = nil
				continue
			}
			spuir, err := s.StakePoolUserInfo(spuim.userAddr)
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("stakePoolUserInfoFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				spuirs[i] = nil
				continue
			}
			connectCount++
			spuirs[i] = spuir
		}

		if connectCount < w.minServers {
			log.Errorf("Unable to check any servers for stakepooluserinfo")
			resp.err = fmt.Errorf("not processing command; %v servers avail is below min of %v", connectCount, w.minServers)
			return resp
		}

		if !w.checkForSyncness(spuirs) {
			log.Infof("StakePoolUserInfo across wallets are not synced.  Attempting to sync now")
			w.syncTickets(spuirs)
		}

		for i := range spuirs {
			if spuirs[i] != nil {
				resp.userInfo = spuirs[i]
				break
			}
		}
		return resp
	case getBestBlockFn:
		resp := new(getBestBlockResponse)
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			hash, height, err := s.GetBestBlock()
			if err != nil && (err != dcrrpcclient.ErrClientDisconnect &&
				err != dcrrpcclient.ErrClientShutdown) {
				log.Infof("getBestBlockFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == dcrrpcclient.ErrClientDisconnect ||
				err == dcrrpcclient.ErrClientShutdown) {
				continue
			}
			resp.bestBlockHeight = height
			resp.bestBlockHash = hash
			return resp
		}
		log.Errorf("Unable to check any servers for getBestBlockFn")
		resp.err = fmt.Errorf("unable to get best block")
		return resp

	}

	return nil
}

// ping pings all the servers and makes sure they're online. This should be
// performed before doing a write.
func (w *walletSvrManager) connected() ([]*dcrjson.WalletInfoResult, error) {
	reply := make(chan connectedResponse)
	w.msgChan <- connectedMsg{
		reply: reply,
	}
	response := <-reply
	return response.walletInfo, response.err
}

// syncTickets is called when checkForSyncness has returned false and the wallets
// PoolTickets need to be synced due to manual addtickets, or a status is off.
// If a ticket is seen to be valid in 1 wallet and invalid in another, we use
// addticket rpc command to add that ticket to the invalid wallet.
func (w *walletSvrManager) syncTickets(spuirs []*dcrjson.StakePoolUserInfoResult) error {
	for i := 0; i < len(spuirs); i++ {
		if w.servers[i] == nil {
			continue
		}
		for j := 0; j < len(spuirs); j++ {
			if w.servers[j] == nil {
				continue
			}
			if i == j {
				continue
			}
			for _, validTicket := range spuirs[i].Tickets {
				for _, invalidTicket := range spuirs[j].InvalidTickets {
					if validTicket.Ticket == invalidTicket {
						hash, err := chainhash.NewHashFromStr(validTicket.Ticket)
						if err != nil {
							return err
						}
						tx, err := w.fetchTransaction(hash)
						if err != nil {
							return err
						}

						log.Infof("adding formally invalid ticket %v to %v", hash, i)
						err = w.servers[j].AddTicket(tx)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// checkForSyncness is a helper function to iterate through results
// of StakePoolUserInfo requests from all the wallets and ensure
// that each share the others PoolTickets and have the same
// valid/invalid lists.  If any thing is deemed off then syncTickets
// call is made.
func (w *walletSvrManager) checkForSyncness(spuirs []*dcrjson.StakePoolUserInfoResult) bool {
	for i := 0; i < len(spuirs); i++ {
		if spuirs[i] == nil {
			continue
		}
		for k := 0; k < len(spuirs); k++ {
			if spuirs[k] == nil {
				continue
			}
			if &spuirs[i] == &spuirs[k] {
				continue
			}
			if len(spuirs[i].Tickets) != len(spuirs[k].Tickets) {
				log.Infof("valid tickets len don't match! server %v has %v "+
					"server %v has %v", i, len(spuirs[i].Tickets), k,
					len(spuirs[k].Tickets))
				return false
			}
			if len(spuirs[i].InvalidTickets) != len(spuirs[k].InvalidTickets) {
				log.Infof("invalid tickets len don't match! server %v has %v "+
					"server %v has %v", i, len(spuirs[i].Tickets), k,
					len(spuirs[k].Tickets))
				return false
			}
			/* TODO
			// for now we are going to just consider the situation where the
			// lengths of invalid/valid tickets differ.  When we have
			// better infrastructure in stakepool wallets to update pool
			// ticket status we can dig deeper into the scenarios and
			// how best to resolve them.
			for y := range spuirs[i].Tickets {
				found := false
				for z := range spuirs[k].Tickets {
					if spuirs[i].Tickets[y] == spuirs[k].Tickets[z] {
						found = true
						break
					}
				}
				if !found {
					log.Infof("ticket not found! %v %v", i, spuirs[i].Tickets[y])
					return false
				}
			}
			for y := range spuirs[i].InvalidTickets {
				found := false
				for z := range spuirs[k].InvalidTickets {
					if spuirs[i].InvalidTickets[y] == spuirs[k].InvalidTickets[z] {
						found = true
						break
					}
				}
				if !found {
					log.Infof("invalid ticket not found! %v %v", i, spuirs[i].InvalidTickets[y])
					return false
				}
			}
			*/
		}
	}
	return true
}

// ValidateAddress
//
// This should return equivalent results from all wallet RPCs. If this
// encounters a failure, it should be considered fatal.
func (w *walletSvrManager) ValidateAddress(addr dcrutil.Address) (*dcrjson.ValidateAddressWalletResult, error) {
	// Assert that all servers are online.
	_, err := w.connected()
	if err != nil {
		return nil, connectionError(err)
	}

	reply := make(chan validateAddressResponse)
	w.msgChan <- validateAddressMsg{
		address: addr,
		reply:   reply,
	}
	response := <-reply
	return response.addrInfo, response.err
}

// CreateMultisig
//
// This should return equivalent results from all wallet RPCs. If this
// encounters a failure, it should be considered fatal.
func (w *walletSvrManager) CreateMultisig(nreq int, addrs []dcrutil.Address) (*dcrjson.CreateMultiSigResult, error) {
	// Assert that all servers are online.
	_, err := w.connected()
	if err != nil {
		return nil, connectionError(err)
	}

	reply := make(chan createMultisigResponse)
	w.msgChan <- createMultisigMsg{
		required:  nreq,
		addresses: addrs,
		reply:     reply,
	}
	response := <-reply
	return response.multisigInfo, response.err
}

// ImportScript
//
// This should return equivalent results from all wallet RPCs. If this
// encounters a failure, it should be considered fatal.
func (w *walletSvrManager) ImportScript(script []byte, height int) error {
	// Assert that all servers are online.
	_, err := w.connected()
	if err != nil {
		return connectionError(err)
	}

	reply := make(chan importScriptResponse)
	w.msgChan <- importScriptMsg{
		height: height,
		script: script,
		reply:  reply,
	}
	response := <-reply
	return response.err
}

// TicketsForAddress
//
// This can race depending on what wallet is currently processing, so failures
// from this function should NOT cause fatal errors on the web server like the
// other RPC client calls.
func (w *walletSvrManager) TicketsForAddress(address dcrutil.Address) (*dcrjson.TicketsForAddressResult, error) {
	w.cachedGetTicketsMutex.Lock()
	defer w.cachedGetTicketsMutex.Unlock()

	// See if we already have a cached copy of this information.
	// If it isn't too old, return that instead.
	cachedResp, ok := w.cachedGetTicketsMap[address.EncodeAddress()]
	if ok {
		if time.Since(cachedResp.timer) < cacheTimerGetTickets {
			return cachedResp.res, nil
		}
	}

	reply := make(chan ticketsForAddressResponse)
	w.msgChan <- ticketsForAddressMsg{
		address: address,
		reply:   reply,
	}
	response := <-reply

	// If there was no error, cache the response now.
	if response.err != nil {
		w.cachedGetTicketsMap[address.EncodeAddress()] =
			NewGetTicketsCacheData(response.tickets)
	}

	return response.tickets, response.err
}

// GetTxOut gets a txOut status given a hash and an output index. It returns
// nothing if the output is spent, and a standard response if it is unspent.
//
// This can race depending on what wallet is currently processing, so failures
// from this function should NOT cause fatal errors on the web server like the
// other RPC client calls.
func (w *walletSvrManager) GetTxOut(hash *chainhash.Hash, idx uint32) (*dcrjson.GetTxOutResult, error) {
	reply := make(chan getTxOutResponse)
	w.msgChan <- getTxOutMsg{
		hash:  hash,
		idx:   idx,
		reply: reply,
	}
	response := <-reply
	return response.txOut, response.err
}

// StakePoolUserInfo gets the stake pool user information for a given user.
//
// This can race depending on what wallet is currently processing, so failures
// from this function should NOT cause fatal errors on the web server like the
// other RPC client calls.
func (w *walletSvrManager) StakePoolUserInfo(userAddr dcrutil.Address) (*dcrjson.StakePoolUserInfoResult, error) {
	reply := make(chan stakePoolUserInfoResponse)
	w.msgChan <- stakePoolUserInfoMsg{
		userAddr: userAddr,
		reply:    reply,
	}
	response := <-reply
	return response.userInfo, response.err
}

// GetBestBlock gets the current best block according the first wallet asked.
func (w *walletSvrManager) GetBestBlock() (*chainhash.Hash, int64, error) {
	reply := make(chan getBestBlockResponse)
	w.msgChan <- getBestBlockMsg{
		reply: reply,
	}
	response := <-reply

	return response.bestBlockHash, response.bestBlockHeight, response.err
}

// getStakeInfo returns the cached current stake statistics about the wallet if
// it has been less than five minutes. If it has been longer than five minutes,
// a new request for stake information is piped through the RPC client handler
// and then cached for future reuse.
//
// This can race depending on what wallet is currently processing, so failures
// from this function should NOT cause fatal errors on the web server like the
// other RPC client calls.
func (w *walletSvrManager) getStakeInfo() (*dcrjson.GetStakeInfoResult, error) {
	// Less than five minutes has elapsed since the last call. Return
	// the previously cached stake information.
	if time.Since(w.cachedStakeInfoTimer) < cacheTimerStakeInfo {
		return w.cachedStakeInfo, nil
	}

	// Five minutes or more has passed since the last call, so request new
	// stake information.
	reply := make(chan getStakeInfoResponse)
	w.msgChan <- getStakeInfoMsg{
		reply: reply,
	}
	response := <-reply

	// If there was an error, return the error and do not reset
	// the timer.
	if response.err != nil {
		return nil, response.err
	}

	// Cache the response for future use and reset the timer.
	w.cachedStakeInfo = response.stakeInfo
	w.cachedStakeInfoTimer = time.Now()

	return response.stakeInfo, nil
}

// GetStakeInfo is the concurrency safe, exported version of getStakeInfo.
func (w *walletSvrManager) GetStakeInfo() (*dcrjson.GetStakeInfoResult, error) {
	w.cachedStakeInfoMutex.Lock()
	defer w.cachedStakeInfoMutex.Unlock()

	return w.getStakeInfo()
}

// getTicketsCacheData is a TicketsForAddressResult that also contains a time
// at which TicketsForAddress was last called. The results should only update.
type getTicketsCacheData struct {
	res   *dcrjson.TicketsForAddressResult
	timer time.Time
}

// NewGetTicketsCacheData is a contructor for getTicketsCacheData that sets the
// last get time to now.
func NewGetTicketsCacheData(tfar *dcrjson.TicketsForAddressResult) *getTicketsCacheData {
	return &getTicketsCacheData{tfar, time.Now()}
}

// walletSvrManager provides a concurrency safe RPC call manager for handling
// all incoming wallet server requests.
type walletSvrManager struct {
	servers    []*dcrrpcclient.Client
	serversLen int

	walletHosts     []string
	walletCerts     []string
	walletUsers     []string
	walletPasswords []string

	walletsLock sync.Mutex

	// cachedStakeInfo is cached information about the stake pool wallet.
	// This is required because of the time it takes to compute the
	// stake information. The included timer is used so that new stake
	// information is only queried for if 5 minutes or more has passed.
	// The mutex is used to allow concurrent access to the stake
	// information if less than five minutes has passed.
	cachedStakeInfo      *dcrjson.GetStakeInfoResult
	cachedStakeInfoTimer time.Time
	cachedStakeInfoMutex sync.Mutex

	// cachedGetTicketsMap caches TicketsForAddress responses and
	// is used to only provide new calls to the wallet RPC after a
	// cooldown period to prevent DoS attacks.
	cachedGetTicketsMap   map[string]*getTicketsCacheData
	cachedGetTicketsMutex sync.Mutex

	// minServers is the minimum number of servers required before alerting
	minServers int

	started  int32
	shutdown int32
	msgChan  chan interface{}
	wg       sync.WaitGroup
	quit     chan struct{}
}

// Start begins the core block handler which processes block and inv messages.
func (w *walletSvrManager) Start() {
	// Already started?
	if atomic.AddInt32(&w.started, 1) != 1 {
		return
	}

	log.Info("Starting wallet RPC manager")
	w.wg.Add(1)
	go w.walletRPCHandler()
}

// Stop gracefully shuts down the block manager by stopping all asynchronous
// handlers and waiting for them to finish.
func (w *walletSvrManager) Stop() error {
	if atomic.AddInt32(&w.shutdown, 1) != 1 {
		log.Info("Wallet RPC manager is already in the process of " +
			"shutting down")
		return nil
	}

	log.Info("Wallet RPC manager shutting down")
	close(w.quit)
	w.wg.Wait()
	return nil
}

// IsStopped returns whether the shutdown field has been engaged.
func (w *walletSvrManager) IsStopped() bool {
	return w.shutdown == 1
}

func (w *walletSvrManager) CheckServers() error {
	if w.serversLen == 0 {
		return fmt.Errorf("No RPC servers")
	}

	for i := range w.servers {
		wi, err := w.servers[i].WalletInfo()
		if err != nil {
			return err
		}
		if !wi.DaemonConnected {
			return fmt.Errorf("Wallet on svr %d not connected\n", i)
		}
		if !wi.Unlocked {
			return fmt.Errorf("Wallet on svr %d not unlocked.\n", i)
		}
	}

	return nil
}

// CheckWalletsReady is a way to verify that each wallets' stake manager is up
// and running, before walletRPCHandler has been started running.
func (w *walletSvrManager) CheckWalletsReady() error {
	if w.serversLen == 0 {
		return fmt.Errorf("No RPC servers")
	}

	for i, s := range w.servers {
		_, err := s.GetStakeInfo()
		if err != nil {
			log.Errorf("GetStakeInfo failured on server %v: %v", i, err)
			return err
		}
	}
	return nil
}

func getMinedTickets(cl *dcrrpcclient.Client, th []*chainhash.Hash) []*chainhash.Hash {
	var ticketHashesMined []*chainhash.Hash
	for _, th := range th {
		res, err := cl.GetRawTransactionVerbose(th)
		if err == nil && res.Confirmations > 0 {
			ticketHashesMined = append(ticketHashesMined, th)
		}
	}
	return ticketHashesMined
}

// GetUnspentUserTickets gets live and immature tickets for a stakepool user
func (w *walletSvrManager) GetUnspentUserTickets(userMultiSigAddress dcrutil.Address) ([]*chainhash.Hash, error) {
	// live tickets only
	var tickethashes []*chainhash.Hash

	// TicketsForAddress returns all tickets, not just live, when wallet is
	// queried rather than just the node. With StakePoolUserInfo, "live" status
	// includes immature, but not spent.
	spui, err := w.StakePoolUserInfo(userMultiSigAddress)
	if err != nil {
		return tickethashes, err
	}

	for _, ticket := range spui.Tickets {
		// "live" includes immature
		if ticket.Status == "live" {
			th, err := chainhash.NewHashFromStr(ticket.Ticket)
			if err != nil {
				log.Errorf("NewHashFromStr failed for %v", ticket)
				return tickethashes, err
			}
			tickethashes = append(tickethashes, th)
		}
	}

	return tickethashes, nil
}

func (w *walletSvrManager) WalletStatus() ([]*dcrjson.WalletInfoResult, error) {
	return w.connected()
}

func getWalletVoteVersion(client *dcrrpcclient.Client) (uint32, error) {
	wi, err := client.WalletInfo()
	if err != nil {
		return 0, err
	}

	return wi.VoteVersion, nil
}

// checkIfWalletConnected checks to see if the passed wallet's client is connected
// and if the wallet is unlocked.
func checkIfWalletConnected(client *dcrrpcclient.Client) error {
	wi, err := client.WalletInfo()
	if err != nil {
		return err
	}
	if !wi.DaemonConnected {
		return fmt.Errorf("wallet not connected")
	}
	if !wi.Unlocked {
		return fmt.Errorf("wallet not unlocked")
	}

	return nil
}

// fetchTransaction cycles through all servers and attempts to review a
// transaction. It returns a not found error if the transaction is
// missing.
func (w *walletSvrManager) fetchTransaction(txHash *chainhash.Hash) (*dcrutil.Tx,
	error) {
	var tx *dcrutil.Tx
	var err error
	for i := range w.servers {
		if w.servers[i] == nil {
			continue
		}
		tx, err = w.servers[i].GetRawTransaction(txHash)
		if err != nil {
			continue
		}

		break
	}

	if tx == nil {
		return nil, fmt.Errorf("couldn't find transaction on any server or " +
			"server failure")
	}

	return tx, nil
}

// checkWalletsVoteVersion returns a consistent vote version between all wallets
// or an error indicating a mismatch
func checkWalletsVoteVersion(wsm *walletSvrManager) (uint32, error) {
	defaultVoteVersion := uint32(0)
	walletVoteVersions := make(map[int]uint32)

	// grab Vote Version from all wallets
	for i := range wsm.servers {
		if wsm.servers[i] == nil {
			continue
		}

		wvv, err := getWalletVoteVersion(wsm.servers[i])
		if err != nil {
			return defaultVoteVersion, err
		}
		walletVoteVersions[i] = wvv
	}

	// ensure Vote Version matches on all wallets
	lastVersion := uint32(0)
	lastServer := 0
	firstrun := true
	for k, v := range walletVoteVersions {
		if firstrun {
			firstrun = false
			lastVersion = v
		}

		if v != lastVersion {
			vErr := fmt.Errorf("wallets %d and %d have mismatched vote versions",
				k, lastServer)
			return defaultVoteVersion, vErr
		}

		lastServer = k
	}

	return lastVersion, nil
}

// walletSvrsSync ensures that the wallet servers are all in sync with each
// other in terms of redeemscripts and address indexes.
func walletSvrsSync(wsm *walletSvrManager, multiSigScripts []models.User) error {
	if wsm.serversLen == 1 {
		return nil
	}

	// Check for connectivity and if unlocked.
	for i := range wsm.servers {
		if wsm.servers[i] == nil {
			continue
		}
		err := checkIfWalletConnected(wsm.servers[i])
		if err != nil {
			return fmt.Errorf("failure on startup sync: %s",
				err.Error())
		}
	}

	type ScriptHeight struct {
		Script []byte
		Height int
	}

	for i := range wsm.servers {
		if wsm.servers[i] == nil {
			continue
		}
		// Set watched address index to MaxUsers so all generated ticket
		// addresses show as 'ismine'.
		err := wsm.servers[i].AccountSyncAddressIndex(defaultAccountName,
			udb.ExternalBranch, MaxUsers)
		if err != nil {
			return err
		}
	}

	// Fetch the redeem scripts from each server.
	redeemScriptsPerServer := make([]map[chainhash.Hash]*ScriptHeight,
		wsm.serversLen)
	allRedeemScripts := make(map[chainhash.Hash]*ScriptHeight)

	// add all scripts from db
	for _, v := range multiSigScripts {
		byteScript, err := hex.DecodeString(v.MultiSigScript)
		if err != nil {
			log.Warnf("skipping script %s due to err %v", v.MultiSigScript, err)
			continue
		}
		allRedeemScripts[chainhash.HashH(byteScript)] = &ScriptHeight{byteScript, int(v.HeightRegistered)}
	}
	// Go through each server and see who is synced to the most redeemscripts.
	log.Info("Getting importscript information from wallets")
	for i := range wsm.servers {
		if wsm.servers[i] == nil {
			continue
		}
		redeemScripts, err := wsm.servers[i].ListScripts()
		if err != nil {
			return err
		}

		redeemScriptsPerServer[i] = make(map[chainhash.Hash]*ScriptHeight)
		for j := range redeemScripts {
			redeemScriptsPerServer[i][chainhash.HashH(redeemScripts[j])] = &ScriptHeight{redeemScripts[j], 0}
			_, ok := allRedeemScripts[chainhash.HashH(redeemScripts[j])]
			if !ok {
				allRedeemScripts[chainhash.HashH(redeemScripts[j])] = &ScriptHeight{redeemScripts[j], 0}
			}
		}
	}

	// Synchronize the address indexes if needed, then synchronize the
	// redeemscripts. Ignore the errors when importing scripts and
	// assume it'll just skip reimportation if it already has it.
	log.Info("Syncing wallets' redeem scripts")
	desynced := false
	for i := range wsm.servers {
		if wsm.servers[i] == nil {
			continue
		}

		// Sync redeemscripts.
		for k, v := range allRedeemScripts {
			_, ok := redeemScriptsPerServer[i][k]
			if !ok {
				log.Infof("RedeemScript from DB not found on server %v. importscript for %x at height %v", i, v.Script, v.Height)
				err := wsm.servers[i].ImportScriptRescanFrom(v.Script, true, v.Height)
				if err != nil {
					return err
				}
				desynced = true
			}
		}
	}

	// If we had to sync then we might be missing some tickets.
	// Scan for the tickets now and try to import any that another wallet may
	// be missing.
	// TODO we should block until the rescans triggered by importing the scripts
	// have been completed.
	if desynced {
		log.Infof("desynced had been detected, now attempting to " +
			"resync all tickets acrosss each wallet.")
		ticketsPerServer := make([]map[chainhash.Hash]struct{},
			wsm.serversLen)
		allTickets := make(map[chainhash.Hash]struct{})

		// Get the tickets and popular the maps.
		for i := range wsm.servers {
			if wsm.servers[i] == nil {
				continue
			}
			ticketsServer, err := wsm.servers[i].GetTickets(true)
			if err != nil {
				return err
			}

			ticketsPerServer[i] = make(map[chainhash.Hash]struct{})
			for j := range ticketsServer {
				ticketHash := ticketsServer[j]
				ticketsPerServer[i][*ticketHash] = struct{}{}
				allTickets[*ticketHash] = struct{}{}
			}
		}

		// Look up the tickets and insert them into the servers
		// that are missing them.
		// TODO Don't look up more than once (cache)
		for i := range wsm.servers {
			if wsm.servers[i] == nil {
				continue
			}
			for ticketHash := range allTickets {
				_, ok := ticketsPerServer[i][ticketHash]
				if !ok {
					h := ticketHash
					log.Infof("wallet %v: is missing ticket %v", i, h)
					tx, err := wsm.fetchTransaction(&h)
					if err != nil {
						return err
					}

					err = wsm.servers[i].AddTicket(tx)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (w *walletSvrManager) DisconnectWalletRPC(serverIndex int) {
	w.walletsLock.Lock()
	defer w.walletsLock.Unlock()
	w.servers[serverIndex] = nil
}

func (w *walletSvrManager) ReconnectWalletRPC(serverIndex int) error {
	w.walletsLock.Lock()
	defer w.walletsLock.Unlock()
	var err error
	if w.servers[serverIndex] == nil {
		w.servers[serverIndex], err = connectWalletRPC(w.walletHosts[serverIndex], w.walletCerts[serverIndex], w.walletUsers[serverIndex], w.walletPasswords[serverIndex])
		if err != nil {
			return err
		}
	}
	return nil
}

func connectWalletRPC(walletHost string, walletCert string, walletUser string, walletPassword string) (*dcrrpcclient.Client, error) {
	certs, err := ioutil.ReadFile(walletCert)
	if err != nil {
		log.Errorf("Error %v", err)
	}
	connCfg := &dcrrpcclient.ConnConfig{
		Host:                 walletHost,
		Endpoint:             "ws",
		User:                 walletUser,
		Pass:                 walletPassword,
		Certificates:         certs,
		DisableAutoReconnect: true,
	}

	client, err := dcrrpcclient.New(connCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("RPC server connection failure on start %v", err)
	}
	return client, nil
}

// newWalletSvrManager returns a new decred wallet server manager.
// Use Start to begin processing asynchronous block and inv updates.
func newWalletSvrManager(walletHosts []string, walletCerts []string,
	walletUsers []string, walletPasswords []string, minServers int) (*walletSvrManager, error) {

	var err error
	localServers := make([]*dcrrpcclient.Client, len(walletHosts))
	for i := range walletHosts {
		localServers[i], err = connectWalletRPC(walletHosts[i], walletCerts[i], walletUsers[i], walletPasswords[i])
		if err != nil {
			log.Infof("couldn't connect to RPC server #%v: %v", walletHosts[i], err)
			return nil, err
		}
	}

	wsm := walletSvrManager{
		walletHosts:          walletHosts,
		walletCerts:          walletCerts,
		walletUsers:          walletUsers,
		walletPasswords:      walletPasswords,
		servers:              localServers,
		serversLen:           len(localServers),
		cachedStakeInfoTimer: time.Now().Add(-cacheTimerStakeInfo),
		cachedGetTicketsMap:  make(map[string]*getTicketsCacheData),
		msgChan:              make(chan interface{}, 500),
		quit:                 make(chan struct{}),
		minServers:           minServers,
	}

	return &wsm, nil
}
