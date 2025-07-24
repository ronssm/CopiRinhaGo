package models

type Payment struct {
    ID            int64   `json:"id"`
    CorrelationID string  `json:"correlationId"`
    Amount        float64 `json:"amount"`
    Processor     string  `json:"processor"` // "default" or "fallback"
    RequestedAt   string  `json:"requestedAt"`
}
