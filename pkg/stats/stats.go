package stats

import (
	"sync"
	"time"

	"CopiRinhaGo/pkg/models"
	"github.com/shopspring/decimal"
)

type PaymentRecord struct {
	CorrelationID string
	Amount        decimal.Decimal
	ProcessorType string
	Timestamp     time.Time
}

type PaymentStats struct {
	mu       sync.RWMutex
	payments []PaymentRecord
}

var globalStats = &PaymentStats{
	payments: make([]PaymentRecord, 0),
}

func RecordPayment(correlationID string, amount decimal.Decimal, processorType string) {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()
	
	record := PaymentRecord{
		CorrelationID: correlationID,
		Amount:        amount,
		ProcessorType: processorType,
		Timestamp:     time.Now(),
	}
	
	globalStats.payments = append(globalStats.payments, record)
}

func GetPaymentsSummary(start, end time.Time) *models.PaymentsSummaryResponse {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()

	defaultRequests := 0
	fallbackRequests := 0
	defaultAmount := decimal.Zero
	fallbackAmount := decimal.Zero

	for _, payment := range globalStats.payments {
		includePayment := start.IsZero() || payment.Timestamp.After(start) || payment.Timestamp.Equal(start)
		
		if includePayment && (payment.Timestamp.Before(end) || payment.Timestamp.Equal(end)) {
			if payment.ProcessorType == "default" {
				defaultRequests++
				defaultAmount = defaultAmount.Add(payment.Amount)
			} else if payment.ProcessorType == "fallback" {
				fallbackRequests++
				fallbackAmount = fallbackAmount.Add(payment.Amount)
			}
		}
	}

	return &models.PaymentsSummaryResponse{
		Default: models.PaymentsSummary{
			TotalRequests: defaultRequests,
			TotalAmount:   defaultAmount,
		},
		Fallback: models.PaymentsSummary{
			TotalRequests: fallbackRequests,
			TotalAmount:   fallbackAmount,
		},
	}
}

func GetPaymentsCount() int {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()
	return len(globalStats.payments)
}
