package poolapi

import (
	"encoding/json"
)

type Response struct {
	Status  string           `json:"status"`
	Message string           `json:"message"`
	Data    *json.RawMessage `json:"data,omitempty"`
}

// TODO: make JSON tags lower-case and add "_" between words

type PurchaseInfo struct {
	PoolAddress   string `json:"PoolAddress"`
	PoolFees      string `json:"PoolFees"`
	Script        string `json:"Script"`
	TicketAddress string `json:"TicketAddress"`
}

type Stats struct {
	AllMempoolTix    string `json:"AllMempoolTix"`
	BlockHeight      string `json:"BlockHeight"`
	Difficulty       string `json:"Difficulty"`
	Immature         string `json:"Immature"`
	Live             string `json:"Live"`
	Missed           string `json:"Missed"`
	OwnMempoolTix    string `json:"OwnMempoolTix"`
	PoolSize         string `json:"PoolSize"`
	ProportionLive   string `json:"ProportionLive"`
	ProportionMissed string `json:"ProportionMissed"`
	Revoked          string `json:"Revoked"`
	TotalSubsidy     string `json:"TotalSubsidy"`
	Voted            string `json:"Voted"`
	Network          string `json:"Network"`
	PoolEmail        string `json:"PoolEmail"`
	PoolFees         string `json:"PoolFees"`
	PoolStatus       string `json:"PoolStatus"`
	UserCount        string `json:"UserCount"`
	UserCountActive  string `json:"UserCountActive"`
	Version          string `json:"Version"`
}
