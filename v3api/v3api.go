package v3api

import "github.com/decred/dcrstakepool/stakepooldclient"

type V3API struct {
	stakepooldConnMan *stakepooldclient.StakepooldManager
}

func New(stakepooldConnMan *stakepooldclient.StakepooldManager) *V3API {
	return &V3API{
		stakepooldConnMan: stakepooldConnMan,
	}
}
