package v3api

import "github.com/decred/dcrstakepool/stakepooldclient"

type V3API struct {
	stakepooldConnMan     *stakepooldclient.StakepooldManager
	ticketChallengeMaxAge int64
}

func New(stakepooldConnMan *stakepooldclient.StakepooldManager, ticketChallengeMaxAge int64) *V3API {
	return &V3API{
		stakepooldConnMan:     stakepooldConnMan,
		ticketChallengeMaxAge: ticketChallengeMaxAge,
	}
}
