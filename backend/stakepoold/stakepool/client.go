package stakepool

import (
	"context"
	"sync"
	"time"

	"github.com/decred/dcrd/rpcclient/v3"
)

const (
	// connectionRetryInterval is the amount of time to wait in between
	// retries when automatically reconnecting to an RPC server.
	connectionRetryInterval = time.Second * 5
	// disconnectCheckInterval is the amount of time to wait between
	// checks for a disconnection.
	disconnectCheckInterval = time.Second * 10
)

// Client holds the information related to an rpcclient and handles access to
// that client through a mutex.
//
// It should be noted that this is a temporary fix to the problem that rpcclient
// does not return an error when autoreconnect is turned on but the client is
// disconnected. The permanent solution is to change the behaviour of rpccleint.
// TODO: Remove this file.
type Client struct {
	// client is protected by a mutex that must be held for reads/writes.
	client *rpcclient.Client
	mux    sync.RWMutex

	cfg          *rpcclient.ConnConfig
	ntfnHandlers *rpcclient.NotificationHandlers
	stop         chan struct{}

	connected    chan struct{}
	connectedMux sync.Mutex
}

// Connected returns a receiving copy of the current connected channel. If
// disconnected and the channel is not yet blocking, creates a new channel that
// will be closed on a successful reconnect.
func (c *Client) Connected() <-chan struct{} {
	c.connectedMux.Lock()
	defer c.connectedMux.Unlock()
	if c.RPCClient().Disconnected() {
		// Start blocking on connected chan if not already.
		select {
		case <-c.connected:
			c.connected = make(chan struct{})
		default:
		}
	}
	return c.connected
}

// IsConnected checks and returns whethere the client is currently connected.
func (c *Client) IsConnected() bool {
	select {
	case <-c.Connected():
		return true
	default:
		return false
	}
}

// RPCClient allows access to the underlying rpcclient by providing a copy of
// its address.
func (c *Client) RPCClient() *rpcclient.Client {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.client
}

// New creates a new Client and starts the automatic reconnection handler.
// Returns an error if unable to construct a new rpcclient.
func NewClient(ctx context.Context, wg *sync.WaitGroup, cfg *rpcclient.ConnConfig, ntfnHandlers *rpcclient.NotificationHandlers) (*Client, error) {
	client, err := rpcclient.New(cfg, ntfnHandlers)
	if err != nil {
		return nil, err
	}
	c := &Client{
		client:       client,
		cfg:          cfg,
		ntfnHandlers: ntfnHandlers,
		connected:    make(chan struct{}),
	}
	// A closed connected channel indcates successfully connected.
	close(c.connected)
	wg.Add(1)
	go c.autoReconnect(ctx, wg)
	return c, nil
}

// autoReconnect waits for a disconnect or stop. On disconnect it attempts to
// reconnect to the client every connectionRetryInterval.
//
// This function must be run as a goroutine.
func (c *Client) autoReconnect(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
out:
	for {
		select {
		case <-ctx.Done():
			break out
		case <-time.After(disconnectCheckInterval):
			if c.IsConnected() {
				continue out
			}
			// Client is disconnected. Try to reconnect.
		}

	reconnect:
		for {
			select {
			case <-ctx.Done():
				break out
			default:
			}

			// Start a new client.
			client, err := rpcclient.New(c.cfg, c.ntfnHandlers)

			if err != nil {
				log.Warnf("Failed to connect to %s: %v",
					c.cfg.Host, err)

				log.Infof("Retrying connection to %s in "+
					"%s", c.cfg.Host, connectionRetryInterval)
				time.Sleep(connectionRetryInterval)
				continue reconnect
			}

			// Properly shutdown old client.
			c.mux.Lock()
			c.client.Shutdown()
			c.client.WaitForShutdown()
			// Switch the new client with the old, shutdown one.
			c.client = client
			c.mux.Unlock()

			// Close the connected channel so that all waiting
			// processes can continue.
			close(c.connected)

			log.Infof("Reestablished connection to RPC server %s",
				c.cfg.Host)

			// Break out of the reconnect loop back to wait for
			// disconnect again.
			break
		}
	}
	// Stop blocking on connected if blocking, as we will never reconnect again.
	if !c.IsConnected() {
		close(c.connected)
	}
	log.Tracef("RPC client reconnect handler done for %s", c.cfg.Host)
}
