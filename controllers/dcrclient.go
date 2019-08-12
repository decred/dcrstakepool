// dcrclient.go

package controllers

import (
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"

	"github.com/decred/dcrd/rpcclient/v3"
	wallettypes "github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

// functionName
type functionName int

const (
	getStakeInfoFn functionName = iota
	connectedFn
)

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
		walletHosts:     walletHosts,
		walletCerts:     walletCerts,
		walletUsers:     walletUsers,
		walletPasswords: walletPasswords,
		servers:         localServers,
		serversLen:      len(localServers),
		msgChan:         make(chan interface{}, 500),
		quit:            make(chan struct{}),
		minServers:      minServers,
	}

	return &wsm, nil
}
