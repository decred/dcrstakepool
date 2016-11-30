package poolapi

type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:",omitempty"`
}

func NewResponse(status, message string, data interface{}) *Response {
	return &Response{status, message, data}
}

type PurchaseInfo struct {
	PoolAddress   string `json:"pool_address"`
	PoolFees      string `json:"pool_fees"`
	Script        string `json:"script"`
	TicketAddress string `json:"ticket_address"`
}

type Stats struct {
	AllMempoolTix    string `json:"allmempooltix"`
	BlockHeight      string `json:"block_height"`
	Difficulty       string `json:"difficulty"`
	Immature         string `json:"immature"`
	Live             string `json:"live"`
	Missed           string `json:"missed"`
	OwnMempoolTix    string `json:"ownmempooltix"`
	PoolSize         string `json:"pool_size"`
	ProportionLive   string `json:"proportion_live"`
	ProportionMissed string `json:"proportion_missed"`
	Revoked          string `json:"revoked"`
	TotalSubsidy     string `json:"total_subsidy"`
	Voted            string `json:"voted"`
	Network          string `json:"network"`
	PoolEmail        string `json:"pool_email"`
	PoolFees         string `json:"pool_fees"`
	PoolStatus       string `json:"pool_status"`
	UserCount        string `json:"user_count"`
	UserCountActive  string `json:"user_count_active"`
	Version          string `json:"version"`
}
