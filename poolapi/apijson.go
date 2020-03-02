package poolapi

import (
	"encoding/json"
)

// Response is the JSON API response to all requests and holds data related to
// the request if successful.
type Response struct {
	Status  string           `json:"status"`
	Message string           `json:"message"`
	Data    *json.RawMessage `json:"data,omitempty"`
}

// TODO: make JSON tags lower-case and add "_" between words

// PurchaseInfo is a JSON data struct related to a user's ticket purchases.
type PurchaseInfo struct {
	PoolAddress     string  `json:"PoolAddress"`
	PoolFees        float64 `json:"PoolFees"`
	Script          string  `json:"Script"`
	TicketAddress   string  `json:"TicketAddress"`
	VoteBits        uint16  `json:"VoteBits"`
	VoteBitsVersion uint32  `json:"VoteBitsVersion"`
}

// Stats is a JSON data struct with information about the pool.
type Stats struct {
	AllMempoolTix        uint32  `json:"AllMempoolTix"`
	APIVersionsSupported []int   `json:"APIVersionsSupported"`
	BlockHeight          int64   `json:"BlockHeight"`
	Difficulty           float64 `json:"Difficulty"`
	Expired              uint32  `json:"Expired"`
	Immature             uint32  `json:"Immature"`
	Live                 uint32  `json:"Live"`
	Missed               uint32  `json:"Missed"`
	OwnMempoolTix        uint32  `json:"OwnMempoolTix"`
	PoolSize             uint32  `json:"PoolSize"`
	ProportionLive       float64 `json:"ProportionLive"`
	ProportionMissed     float64 `json:"ProportionMissed"`
	Revoked              uint32  `json:"Revoked"`
	TotalSubsidy         float64 `json:"TotalSubsidy"`
	Voted                uint32  `json:"Voted"`
	Network              string  `json:"Network"`
	PoolEmail            string  `json:"PoolEmail"`
	PoolFees             float64 `json:"PoolFees"`
	PoolStatus           string  `json:"PoolStatus"`
	UserCount            int64   `json:"UserCount"`
	UserCountActive      int64   `json:"UserCountActive"`
	Version              string  `json:"Version"`
}
