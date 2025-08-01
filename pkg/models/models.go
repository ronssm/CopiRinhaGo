package models

import (
	"github.com/shopspring/decimal"
	"time"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

type PaymentRequest struct {
	CorrelationID string          `json:"correlationId"`
	Amount        decimal.Decimal `json:"amount"`
	RequestedAt   time.Time       `json:"requestedAt"`
}

type PaymentRecord struct {
	CorrelationID string          `json:"correlationId"`
	Amount        decimal.Decimal `json:"amount"`
	ProcessedAt   time.Time       `json:"processedAt"`
	ProcessorType string          `json:"processorType"`
}

type PaymentsSummary struct {
	TotalRequests int             `json:"totalRequests"`
	TotalAmount   decimal.Decimal `json:"totalAmount"`
}

type PaymentsSummaryResponse struct {
	Default  PaymentsSummary `json:"default"`
	Fallback PaymentsSummary `json:"fallback"`
}

// RPC message types
type EnqueueRPC struct {
	Request *PaymentRequest
}

type DequeueRPC struct {
	Request *PaymentRequest
}

type DequeueBatchRPC struct {
	BatchSize int
	Requests  []*PaymentRequest
}

type StatsRPC struct {
	ProcessorType string
	Amount        decimal.Decimal
	ProcessedAt   time.Time
}

type SummaryRPC struct {
	Start time.Time
	End   time.Time
}
