// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package signal provides a shutdown context and listener.
package signal

import (
	"context"
	"os"
	"os/signal"
)

// shutdownSignaled is closed whenever shutdown is invoked through an interrupt
// signal. Any contexts created using withShutdownChannel are cancelled when
// this is closed.
var shutdownSignaled = make(chan struct{})

// withShutdownCancel creates a copy of a context that is cancelled whenever
// shutdown is invoked through an interrupt signal.
func WithShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignaled
		cancel()
	}()
	return ctx
}

// shutdownListener listens for shutdown requests and cancels all contexts
// created from withShutdownCancel.  This function never returns and is intended
// to be spawned in a new goroutine.
func ShutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	// Only accept a single CTRL+C.
	signal.Notify(interruptChannel, os.Interrupt)

	// Listen for the initial shutdown signal
	sig := <-interruptChannel
	log.Infof("Received signal (%s).  Shutting down...", sig)

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignaled)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		<-interruptChannel
		log.Info("Shutdown signaled.  Already shutting down...")
	}
}
