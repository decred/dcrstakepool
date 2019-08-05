// dcrclient.go

package controllers

import (
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/rpcclient/v3"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/stakepooldclient"
	wallettypes "github.com/decred/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/dcrwallet/wallet/v2/udb"
)

// functionName
type functionName int

const (
	getStakeInfoFn functionName = iota
	connectedFn
)

var (
	// cacheTimerStakeInfo is the duration of time after which to
	// access the wallet and update the stake information instead
	// of returning cached stake information.
	cacheTimerStakeInfo = 5 * time.Minute

	// defaultAccountName is the account name for the default wallet
	// account as a string.
	defaultAccountName = "default"
)

// getStakeInfoResponse
type getStakeInfoResponse struct {
	stakeInfo *wallettypes.GetStakeInfoResult
	err       error
}

// getStakeInfoMsg
type getStakeInfoMsg struct {
	reply chan getStakeInfoResponse
}

// connectedResponse
type connectedResponse struct {
	walletInfo []*wallettypes.WalletInfoResult
	err        error
}

// connectedMsg
type connectedMsg struct {
	reply chan connectedResponse
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
			case getStakeInfoMsg:
				resp := w.executeInSequence(getStakeInfoFn, msg)
				respTyped := resp.(*getStakeInfoResponse)
				msg.reply <- *respTyped
			case connectedMsg:
				resp := w.executeInSequence(connectedFn, msg)
				respTyped := resp.(*connectedResponse)
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
	case getStakeInfoFn:
		resp := new(getStakeInfoResponse)
		gsirs := make([]*wallettypes.GetStakeInfoResult, w.serversLen)
		var connectCount int
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			gsir, err := s.GetStakeInfo()
			if err != nil && (err != rpcclient.ErrClientDisconnect &&
				err != rpcclient.ErrClientShutdown) {
				log.Infof("getStakeInfoFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == rpcclient.ErrClientDisconnect ||
				err == rpcclient.ErrClientShutdown) {
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
		wirs := make([]*wallettypes.WalletInfoResult, w.serversLen)
		resp.walletInfo = wirs
		var connectCount int
		for i, s := range w.servers {
			if w.servers[i] == nil {
				continue
			}
			wir, err := s.WalletInfo()
			if err != nil && (err != rpcclient.ErrClientDisconnect &&
				err != rpcclient.ErrClientShutdown) {
				log.Infof("connectedFn failure on server %v: %v", i, err)
				resp.err = err
				return resp
			} else if err != nil && (err == rpcclient.ErrClientDisconnect ||
				err == rpcclient.ErrClientShutdown) {
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

	}

	return nil
}

// ping pings all the servers and makes sure they're online. This should be
// performed before doing a write.
func (w *walletSvrManager) connected() ([]*wallettypes.WalletInfoResult, error) {
	reply := make(chan connectedResponse)
	w.msgChan <- connectedMsg{
		reply: reply,
	}
	response := <-reply
	return response.walletInfo, response.err
}

// getStakeInfo returns the cached current stake statistics about the wallet if
// it has been less than five minutes. If it has been longer than five minutes,
// a new request for stake information is piped through the RPC client handler
// and then cached for future reuse.
//
// This can race depending on what wallet is currently processing, so failures
// from this function should NOT cause fatal errors on the web server like the
// other RPC client calls.
func (w *walletSvrManager) getStakeInfo() (*wallettypes.GetStakeInfoResult, error) {
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
func (w *walletSvrManager) GetStakeInfo() (*wallettypes.GetStakeInfoResult, error) {
	w.cachedStakeInfoMutex.Lock()
	defer w.cachedStakeInfoMutex.Unlock()

	return w.getStakeInfo()
}

// walletSvrManager provides a concurrency safe RPC call manager for handling
// all incoming wallet server requests.
type walletSvrManager struct {
	servers    []*rpcclient.Client
	serversLen int

	walletHosts     []string
	walletCerts     []string
	walletUsers     []string
	walletPasswords []string

	walletsLock sync.Mutex

	// cachedStakeInfo is cached information about the voting service wallet.
	// This is required because of the time it takes to compute the stake
	// information. The included timer is used so that new stake information is
	// only queried for if 5 minutes or more has passed. The mutex is used to
	// allow concurrent access to the stake information if less than five
	// minutes has passed.
	cachedStakeInfo      *wallettypes.GetStakeInfoResult
	cachedStakeInfoTimer time.Time
	cachedStakeInfoMutex sync.Mutex

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
			return fmt.Errorf("server %d wallet is not connected\n", i)
		}
		if !wi.Unlocked {
			return fmt.Errorf("server %d wallet is not unlocked.\n", i)
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
			log.Errorf("GetStakeInfo failed on server %v: %v", i, err)
			return err
		}
	}
	return nil
}

// checkIfWalletConnected checks to see if the passed wallet's client is connected
// and if the wallet is unlocked.
func checkIfWalletConnected(client *rpcclient.Client) error {
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

// walletSvrsSync ensures that the wallet servers are all in sync with each
// other in terms of redeemscripts and address indexes.
func walletSvrsSync(wsm *walletSvrManager, stakepoold *stakepooldclient.StakepooldManager, multiSigScripts []models.User) error {
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

	// Set watched address index to maxUsers so all generated ticket
	// addresses show as 'ismine'.
	err := stakepoold.SyncWatchedAddresses(defaultAccountName, udb.ExternalBranch, MaxUsers)
	if err != nil {
		return err
	}

	// Synchronize the address indexes, then synchronize the
	// redeemscripts. Ignore the errors when importing scripts and
	// assume it'll just skip reimportation if it already has it.
	err = stakepoold.SyncScripts(multiSigScripts)
	if err != nil {
		return err
	}

	// If we had to sync then we might be missing some tickets.
	// Scan for the tickets now and try to import any that another wallet may
	// be missing.
	err = stakepoold.SyncTickets()
	if err != nil {
		return err
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

func connectWalletRPC(walletHost string, walletCert string, walletUser string, walletPassword string) (*rpcclient.Client, error) {
	certs, err := ioutil.ReadFile(walletCert)
	if err != nil {
		log.Errorf("Error %v", err)
	}
	connCfg := &rpcclient.ConnConfig{
		Host:                 walletHost,
		Endpoint:             "ws",
		User:                 walletUser,
		Pass:                 walletPassword,
		Certificates:         certs,
		DisableAutoReconnect: true,
	}

	client, err := rpcclient.New(connCfg, nil)
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
	localServers := make([]*rpcclient.Client, len(walletHosts))
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
		msgChan:              make(chan interface{}, 500),
		quit:                 make(chan struct{}),
		minServers:           minServers,
	}

	return &wsm, nil
}
