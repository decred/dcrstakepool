package poolapi

type GetPubKeyResponse struct {
	Timestamp int64  `json:"timestamp"`
	PubKey    []byte `json:"pubKey"`
}
