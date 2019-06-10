package main

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v2"
	"github.com/decred/dcrstakepool/backend/stakepoold/rpc/rpcserver"
)

// Define notification handlers
func getNodeNtfnHandlers(ctx *rpcserver.AppContext) *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnNewTickets: func(blockHash *chainhash.Hash, blockHeight int64, _ int64, tickets []*chainhash.Hash) {
			nt := rpcserver.NewTicketsForBlock{
				BlockHash:   blockHash,
				BlockHeight: blockHeight,
				NewTickets:  tickets,
			}
			ctx.NewTicketsChan <- nt
		},
		OnSpentAndMissedTickets: func(blockHash *chainhash.Hash, blockHeight int64, _ int64, tickets map[chainhash.Hash]bool) {
			ticketsFixed := make(map[*chainhash.Hash]bool)
			for ticketHash, spent := range tickets {
				ticketHash := ticketHash
				ticketsFixed[&ticketHash] = spent
			}
			smt := rpcserver.SpentMissedTicketsForBlock{
				BlockHash:   blockHash,
				BlockHeight: blockHeight,
				SmTickets:   ticketsFixed,
			}
			ctx.SpentmissedTicketsChan <- smt
		},
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64, winningTickets []*chainhash.Hash) {
			wt := rpcserver.WinningTicketsForBlock{
				BlockHash:      blockHash,
				BlockHeight:    blockHeight,
				WinningTickets: winningTickets,
			}
			ctx.WinningTicketsChan <- wt
		},
	}
}

func getWalletNtfnHandlers() *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnUnknownNotification: func(method string, params []json.RawMessage) {
			log.Infof("ignoring notification %v", method)
			log.Tracef("%#v", params)
		},
	}
}
