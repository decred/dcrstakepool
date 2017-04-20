package main

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
)

// Define notification handlers
func getNodeNtfnHandlers(ctx *appContext, connCfg *dcrrpcclient.ConnConfig) *dcrrpcclient.NotificationHandlers {
	return &dcrrpcclient.NotificationHandlers{
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64,
			winningTickets []*chainhash.Hash) {
			wt := WinningTicketsForBlock{
				blockHash:      blockHash,
				blockHeight:    blockHeight,
				host:           connCfg.Host,
				winningTickets: winningTickets,
			}
			ctx.winningTicketsChan <- wt
		},
	}
}

func getWalletNtfnHandlers(cfg *config) *dcrrpcclient.NotificationHandlers {
	return &dcrrpcclient.NotificationHandlers{
		OnUnknownNotification: func(method string, params []json.RawMessage) {
			log.Infof("ignoring notification %v", method)
		},
	}
}

func sliceContains(s []*chainhash.Hash, e *chainhash.Hash) bool {
	for _, a := range s {
		if a.IsEqual(e) {
			return true
		}
	}
	return false
}
