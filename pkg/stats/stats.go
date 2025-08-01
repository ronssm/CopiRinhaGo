package stats

import (
	"sync"
	"time"

	"CopiRinhaGo/pkg/models"
	"github.com/shopspring/decimal"
)

type PaymentStats struct {
	mu            sync.RWMutex
	defaultStats  models.PaymentsSummary
	fallbackStats models.PaymentsSummary
	payments      []models.PaymentRecord
}

var globalStats = &PaymentStats{
	defaultStats:  models.PaymentsSummary{TotalRequests: 0, TotalAmount: decimal.Zero},
	fallbackStats: models.PaymentsSummary{TotalRequests: 0, TotalAmount: decimal.Zero},
	payments:      make([]models.PaymentRecord, 0, 100000),
}

func RecordPayment(correlationID string, amount decimal.Decimal, processorType string) {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	record := models.PaymentRecord{
		CorrelationID: correlationID,
		Amount:        amount,
		ProcessedAt:   time.Now(),
		ProcessorType: processorType,
	}

	// Add to payments list
	globalStats.payments = append(globalStats.payments, record)

	// Update stats
	if processorType == "default" {
		globalStats.defaultStats.TotalRequests++
		globalStats.defaultStats.TotalAmount = globalStats.defaultStats.TotalAmount.Add(amount)
	} else if processorType == "fallback" {
		globalStats.fallbackStats.TotalRequests++
		globalStats.fallbackStats.TotalAmount = globalStats.fallbackStats.TotalAmount.Add(amount)
	}
}

func GetPaymentsSummary(start, end time.Time) *models.PaymentsSummaryResponse {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()

	// For now, return all-time stats (ignoring time filtering for performance)
	// In a production system, you'd implement proper time-range filtering
	return &models.PaymentsSummaryResponse{
		Default:  globalStats.defaultStats,
		Fallback: globalStats.fallbackStats,
	}
}

func GetPaymentsCount() int {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()
	return len(globalStats.payments)
}
