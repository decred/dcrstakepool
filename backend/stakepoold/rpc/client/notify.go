// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package client

import (
	"encoding/json"
	"fmt"

	"github.com/decred/dcrstakepool/backend/stakepoold/stakepool"
)

type notifier struct {
	spd        *stakepool.Stakepoold
	walletConn *Conn
}

// NewNotifier returns an initiated notifier.
func NewNotifier(spd *stakepool.Stakepoold, walletConn *Conn) *notifier {
	return &notifier{spd: spd, walletConn: walletConn}
}

// Notify satifies the wsrpc Notify interface. it performs an action when a
// specific notification is received.
func (n *notifier) Notify(method string, params json.RawMessage) error {
	var err error
	switch method {
	case "newtickets":
		err = n.spd.NotificationNewTickets(params)
	case "winningtickets":
		err = n.spd.NotificationWinningTickets(params)
	case "spentandmissedtickets":
		// Wait for a wallet connection if not connected.
		n.walletConn.todofn(func() { err = n.spd.NotificationSpentAndMissedTickets(params) })
	}
	if err != nil {
		err = fmt.Errorf("dcrd notification: %v: %v", method, err)
		log.Error(err)
		return err
	}
	return nil
}
