package main

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
)

// checkIfBlockSeen increments the NeedToVote waitgroup by 1 if the block
// has not been seen yet and records that it has been seen.
func checkIfBlockSeen(ctx *appContext, ntfnName string, blockHash *chainhash.Hash, blockHeight int64) {
	hasBeenSeen := true

	ctx.Lock()
	if !ctx.lastBlockSeenHash.IsEqual(blockHash) || ctx.lastBlockSeenHeight != blockHeight {
		hasBeenSeen = false
		ctx.wgNeedToVote.Add(1)
		ctx.lastBlockSeenHash = blockHash
		ctx.lastBlockSeenHeight = blockHeight
	}
	ctx.Unlock()

	// Log notification information outside of the handler.
	go func() {
		log.Debugf("ntfn %s blockHeight %v blockHash %v", ntfnName, blockHeight,
			blockHash)
		if !hasBeenSeen {
			log.Debugf("incremented wgNeedToVote for block height %v hash %v",
				blockHeight, blockHash)
		}
	}()
}

// Define notification handlers
func getNodeNtfnHandlers(ctx *appContext, connCfg *dcrrpcclient.ConnConfig) *dcrrpcclient.NotificationHandlers {
	return &dcrrpcclient.NotificationHandlers{
		OnNewTickets: func(blockHash *chainhash.Hash, blockHeight int64, stakeDifficulty int64, tickets []*chainhash.Hash) {
			checkIfBlockSeen(ctx, "OnNewTickets", blockHash, blockHeight)
			nt := NewTicketsForBlock{
				blockHash:   blockHash,
				blockHeight: blockHeight,
				newTickets:  tickets,
			}
			ctx.newTicketsChan <- nt
		},
		OnSpentAndMissedTickets: func(blockHash *chainhash.Hash, blockHeight int64, stakeDifficulty int64, tickets map[chainhash.Hash]bool) {
			checkIfBlockSeen(ctx, "OnSpentAndMissedTickets", blockHash, blockHeight)
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
			checkIfBlockSeen(ctx, "OnWinningTickets", blockHash, blockHeight)
			wt := WinningTicketsForBlock{
				blockHash:      blockHash,
				blockHeight:    blockHeight,
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
