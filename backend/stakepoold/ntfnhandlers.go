package main

import (
	"encoding/json"
	"strings"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
)

// Define notification handlers
func getNodeNtfnHandlers(ctx *appContext, connCfg *dcrrpcclient.ConnConfig) *dcrrpcclient.NotificationHandlers {
	return &dcrrpcclient.NotificationHandlers{
		OnWinningTickets: func(blockHash *chainhash.Hash, blockHeight int64,
			winningTickets []*chainhash.Hash) {
			// TODO we really should do the least work possible here and
			// just generate a list of tickets to vote and return
			var txstr []string
			for _, wt := range winningTickets {
				txstr = append(txstr, wt.String())
				for msa := range ctx.userTickets {
					if sliceContains(ctx.userTickets[msa].Tickets, wt) {
						log.Infof("winningTicket %v for height %v hash %v is present on wallet",
							wt, blockHeight, blockHash)
						sstx, err := walletCreateVote(ctx, blockHash, blockHeight, wt, msa)
						if err != nil {
							log.Infof("failed to create vote: %v", err)
						} else {
							log.Infof("created vote %v", sstx)
							// TODO this should really be sent to dcrd, not dcrwallet
							// but we can't do that from here since our dcrd connection
							// is blocked while processing this notification
							txHex, err := walletSendVote(ctx, sstx)
							if err != nil {
								log.Infof("failed to vote: %v", err)
							} else {
								log.Infof("sent vote ok hex %v", txHex)
							}
						}
					}
				}
			}
			log.Debugf("OnWinningTickets from %v tickets for height %v: %v",
				connCfg.Host, blockHeight, strings.Join(txstr, ", "))
			// TODO we don't want to do this every block.  otherwise if 2 blocks
			// come in close together, we may vote late on the 2nd block.
			// maybe a config option that does it on even/odd blocks so not all
			// wallets are updating every block?
			// we also don't want to do this from the notification handler.
			ctx.userTickets = walletFetchUserTickets(ctx)
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
