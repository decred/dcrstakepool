package poolapi

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    string `json:"omitempty"`
}
