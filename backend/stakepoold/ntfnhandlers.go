package main

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v3"
	"github.com/decred/dcrstakepool/backend/stakepoold/stakepool"
)

// Define notification handlers
func getNodeNtfnHandlers(spd *stakepool.Stakepoold) *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnNewTickets: func(blockHash *chainhash.Hash, blockHeight int64, _ int64, tickets []*chainhash.Hash) {
			nt := stakepool.NewTicketsForBlock{
				BlockHash:   blockHash,
				BlockHeight: blockHeight,
				NewTickets:  tickets,
			}
			spd.NewTicketsChan <- nt
		},
		OnSpentAndMissedTickets: func(blockHash *chainhash.Hash, blockHeight int64, _ int64, tickets map[chainhash.Hash]bool) {
			ticketsFixed := make(map[*chainhash.Hash]bool)
			for ticketHash, spent := range tickets {
				ticketHash := ticketHash
				ticketsFixed[&ticketHash] = spent
			}
			smt := stakepool.SpentMissedTicketsForBlock{
				BlockHash:   blockHash,
				BlockHeight: blockHeight,
				SmTickets:   ticketsFixed,
			}
			// Wait for a wallet connection if not connected.
			<-spd.WalletConnection.Connected()
			spd.SpentmissedTicketsChan <- smt
		},
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64, winningTickets []*chainhash.Hash) {
			wt := stakepool.WinningTicketsForBlock{
				BlockHash:      blockHash,
				BlockHeight:    blockHeight,
				WinningTickets: winningTickets,
			}
			spd.WinningTicketsChan <- wt
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
