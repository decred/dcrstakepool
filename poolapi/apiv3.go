package poolapi

type GetPubKeyResponse struct {
	Timestamp int64  `json:"timestamp"`
	PubKey    []byte `json:"pubKey"`
}

type GetFeeResponse struct {
	Timestamp int64   `json:"timestamp"`
	Fee       float64 `json:"fee"`
}

type GetFeeAddressResponse struct {
	Timestamp           int64  `json:"timestamp"`
	TicketHash          string `json:"ticketHash"`
	CommitmentSignature string `json:"commitmentSignature"`
	FeeAddress          string `json:"feeAddress"`
}
