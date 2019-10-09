// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package client implements a json rpc client for communication with dcrd and
// dcrwallet.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"sync"
	"time"

	"github.com/jrick/wsrpc/v2"
)

const (
	// connectionRetryInterval is the amount of time to start waiting in
	// between retries when automatically reconnecting to an RPC server.
	connectionRetryInterval = time.Second * 5
)

// RPCOptions specifies the network settings for establishing a websocket
// connection to a JSON-RPC server.
type RPCOptions struct {
	Host string
	User string
	Pass string
	CA   []byte
}

// Conn holds the information related to an rpcclient and handles access to
// that client through a mutex.
type Conn struct {
	// wsclient is protected by a mutex that must be held for reads/writes.
	wsclient *wsrpc.Client
	mux      sync.RWMutex

	host       string
	hostAddr   string
	opts       []wsrpc.Option
	retryCount int64

	// todo will block while disconnected and perform sent functions when
	// connected.
	todo chan func()
	done chan struct{}
}

// Call passes the jspn-RPC call along to the server if connected.  Returns an
// ErrNotConnected if not connected.
func (c *Conn) Call(ctx context.Context, method string, res interface{}, args ...interface{}) error {
	c.mux.RLock()
	wsclient := c.wsclient
	c.mux.RUnlock()
	return wsclient.Call(ctx, method, res, args...)
}

// New creates a new Conn and starts the automatic reconnection handler.
// Returns an error if unable to dial the RPC server.
func NewConn(ctx context.Context, wg *sync.WaitGroup, options *RPCOptions) (*Conn, error) {
	opts := make([]wsrpc.Option, 0, 2)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(options.CA)
	tc := &tls.Config{
		RootCAs: pool,
	}
	opts = append(opts, wsrpc.WithBasicAuth(options.User, options.Pass), wsrpc.WithTLSConfig(tc))
	hostAddr := "wss://" + options.Host + "/ws"
	wsclient, err := wsrpc.Dial(ctx, hostAddr, opts...)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		wsclient: wsclient,
		host:     options.Host,
		hostAddr: hostAddr,
		opts:     opts,
		todo:     make(chan func()),
		done:     make(chan struct{}),
	}
	wg.Add(1)
	go c.autoReconnect(ctx, wg)
	return c, nil
}

// TODO waits and performs a function when connected. Functions will be
// performed in FIFO order upon connection. Will stop blocking and not perform
// functions after autoconnect is stopped.
func (c *Conn) TODO(f func()) {
	select {
	case <-c.done:
	case c.todo <- f:
	}
}

// autoReconnect waits for a disconnect or ctx Done. On disconnect it attempts to
// reconnect to the client every connectionRetryInterval increasing each try until
// one minute.
//
// This function must be run as a goroutine.
func (c *Conn) autoReconnect(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer c.wsclient.Close()
	defer close(c.done)
out:
	for {
		select {
		case <-ctx.Done():
			break out
		case f := <-c.todo:
			f()
			continue
		case <-c.wsclient.Done():
			// wsclient's Done channel should close on disconnect.
			log.Errorf("RPC client disconnected from server at %s: %v", c.host, c.wsclient.Err())
			// Try to reconnect.
		}
	reconnect:
		for {
			select {
			case <-ctx.Done():
				break out
			default:
			}
			// Start a new client.
			wsclient, err := wsrpc.Dial(ctx, c.hostAddr, c.opts...)
			if err != nil {
				log.Warnf("Failed to connect to %s: %v",
					c.host, err)
				c.retryCount++
				// Scale the retry interval by the number of
				// retries so there is a backoff up to a max
				// of 1 minute.
				scaledInterval := connectionRetryInterval.Nanoseconds() * c.retryCount
				scaledDuration := time.Duration(scaledInterval)
				if scaledDuration > time.Minute {
					scaledDuration = time.Minute
				}
				log.Infof("Retrying connection to %s in %s", c.host, scaledDuration)
				select {
				case <-ctx.Done():
					break out
				case <-time.After(scaledDuration):
					continue reconnect
				}
			}
			// Properly shutdown old client.
			c.mux.Lock()
			c.wsclient.Close()
			// Switch the new client with the old one.
			c.wsclient = wsclient
			c.mux.Unlock()
			c.retryCount = 0
			log.Infof("Reestablished connection to RPC server %s",
				c.host)
			// Break out of the reconnect loop back to wait for
			// disconnect again.
			break
		}
	}
	log.Tracef("RPC client reconnect handler done for %s", c.host)
}
