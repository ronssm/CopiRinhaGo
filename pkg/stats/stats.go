package stats

import (
	"sync"
	"sync/atomic"
	"time"

	"CopiRinhaGo/pkg/models"
	"github.com/shopspring/decimal"
)

type PaymentStats struct {
	mu                 sync.RWMutex
	defaultRequests    int64   // Use atomic for better performance
	fallbackRequests   int64   // Use atomic for better performance
	defaultAmount      decimal.Decimal
	fallbackAmount     decimal.Decimal
}

var globalStats = &PaymentStats{
	defaultRequests:  0,
	fallbackRequests: 0,
	defaultAmount:    decimal.Zero,
	fallbackAmount:   decimal.Zero,
}

func RecordPayment(correlationID string, amount decimal.Decimal, processorType string) {
	if processorType == "default" {
		atomic.AddInt64(&globalStats.defaultRequests, 1)
		globalStats.mu.Lock()
		globalStats.defaultAmount = globalStats.defaultAmount.Add(amount)
		globalStats.mu.Unlock()
	} else if processorType == "fallback" {
		atomic.AddInt64(&globalStats.fallbackRequests, 1)
		globalStats.mu.Lock()
		globalStats.fallbackAmount = globalStats.fallbackAmount.Add(amount)
		globalStats.mu.Unlock()
	}
}

func GetPaymentsSummary(start, end time.Time) *models.PaymentsSummaryResponse {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()

	return &models.PaymentsSummaryResponse{
		Default: models.PaymentsSummary{
			TotalRequests: int(atomic.LoadInt64(&globalStats.defaultRequests)),
			TotalAmount:   globalStats.defaultAmount,
		},
		Fallback: models.PaymentsSummary{
			TotalRequests: int(atomic.LoadInt64(&globalStats.fallbackRequests)),
			TotalAmount:   globalStats.fallbackAmount,
		},
	}
}

func GetPaymentsCount() int {
	return int(atomic.LoadInt64(&globalStats.defaultRequests)) + int(atomic.LoadInt64(&globalStats.fallbackRequests))
}
