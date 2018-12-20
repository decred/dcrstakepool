package main

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient"
)

// Define notification handlers
func getNodeNtfnHandlers(ctx *appContext) *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnNewTickets: func(blockHash *chainhash.Hash, blockHeight int64, stakeDifficulty int64, tickets []*chainhash.Hash) {
			nt := NewTicketsForBlock{
				blockHash:   blockHash,
				blockHeight: blockHeight,
				newTickets:  tickets,
			}
			ctx.newTicketsChan <- nt
		},
		OnSpentAndMissedTickets: func(blockHash *chainhash.Hash, blockHeight int64, stakeDifficulty int64, tickets map[chainhash.Hash]bool) {
			ticketsFixed := make(map[*chainhash.Hash]bool)
			for ticketHash, spent := range tickets {
				ticketHash := ticketHash
				ticketsFixed[&ticketHash] = spent
			}
			smt := SpentMissedTicketsForBlock{
				blockHash:   blockHash,
				blockHeight: blockHeight,
				smTickets:   ticketsFixed,
			}
			ctx.spentmissedTicketsChan <- smt
		},
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64, winningTickets []*chainhash.Hash) {
			wt := WinningTicketsForBlock{
				blockHash:      blockHash,
				blockHeight:    blockHeight,
				winningTickets: winningTickets,
			}
			ctx.winningTicketsChan <- wt
		},
	}
}

func getWalletNtfnHandlers() *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnUnknownNotification: func(method string, params []json.RawMessage) {
			log.Infof("ignoring notification %v", method)
		},
	}
}
