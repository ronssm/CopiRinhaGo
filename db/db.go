package db

import (
	"sync"
	"time"
)

type Payment struct {
	CorrelationID string    `json:"correlationId"`
	Amount        float64   `json:"amount"`
	ProcessorType string    `json:"processorType"`
	ProcessedAt   time.Time `json:"processedAt"`
}

type PaymentSummary struct {
	TotalRequests int     `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

var (
	summaryData = map[string]*PaymentSummary{
		"default":  {TotalRequests: 0, TotalAmount: 0},
		"fallback": {TotalRequests: 0, TotalAmount: 0},
	}
	summaryMu sync.RWMutex

	recentPayments = make([]*Payment, 0, 50000)
	paymentsIndex  = 0
	paymentsMu     sync.RWMutex
	summaryUpdateChan = make(chan *Payment, 5000)
	batchSize         = 50
	batchTimeout      = 5 * time.Millisecond
)

func Init() error {
	go summaryBatchProcessor()
	return nil
}

func summaryBatchProcessor() {
	batch := make([]*Payment, 0, batchSize)
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case payment := <-summaryUpdateChan:
			batch = append(batch, payment)

			if len(batch) >= batchSize {
				processSummaryBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				processSummaryBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func processSummaryBatch(payments []*Payment) {
	updates := make(map[string]struct {
		requests int
		amount   float64
	})

	for _, payment := range payments {
		update := updates[payment.ProcessorType]
		update.requests++
		update.amount += payment.Amount
		updates[payment.ProcessorType] = update
	}

	summaryMu.Lock()
	for processorType, update := range updates {
		if summary, exists := summaryData[processorType]; exists {
			summary.TotalRequests += update.requests
			summary.TotalAmount += update.amount
		}
	}
	summaryMu.Unlock()
}

func StorePayment(payment *Payment) error {
	select {
	case summaryUpdateChan <- payment:
	default:
		summaryMu.Lock()
		if summary, exists := summaryData[payment.ProcessorType]; exists {
			summary.TotalRequests++
			summary.TotalAmount += payment.Amount
		}
		summaryMu.Unlock()
	}

	paymentsMu.Lock()
	if len(recentPayments) < cap(recentPayments) {
		recentPayments = append(recentPayments, payment)
	} else {
		recentPayments[paymentsIndex] = payment
		paymentsIndex = (paymentsIndex + 1) % len(recentPayments)
	}
	paymentsMu.Unlock()

	return nil
}

func GetPaymentsSummary(processorType string, fromTime, toTime *time.Time) (*PaymentSummary, error) {
	summaryMu.RLock()
	summary := summaryData[processorType]
	result := &PaymentSummary{
		TotalRequests: summary.TotalRequests,
		TotalAmount:   summary.TotalAmount,
	}
	summaryMu.RUnlock()

	return result, nil
}

func GetOverallPaymentsSummary() (interface{}, error) {
	summaryMu.RLock()
	defaultSummary := summaryData["default"]
	fallbackSummary := summaryData["fallback"]

	result := map[string]interface{}{
		"default": map[string]interface{}{
			"totalRequests": defaultSummary.TotalRequests,
			"totalAmount":   defaultSummary.TotalAmount,
		},
		"fallback": map[string]interface{}{
			"totalRequests": fallbackSummary.TotalRequests,
			"totalAmount":   fallbackSummary.TotalAmount,
		},
	}
	summaryMu.RUnlock()

	return result, nil
}

func GetOverallPaymentsSummaryWithTimeRange(fromTime, toTime *time.Time) (interface{}, error) {
	return GetOverallPaymentsSummary()
}

func Close() error {
	return nil
}

func GetMemoryStats() map[string]interface{} {
	paymentsMu.RLock()
	paymentCount := len(recentPayments)
	paymentsMu.RUnlock()

	summaryMu.RLock()
	summaryCount := len(summaryData)
	summaryMu.RUnlock()

	return map[string]interface{}{
		"stored_payments":     paymentCount,
		"max_payments":        cap(recentPayments),
		"summary_processors":  summaryCount,
		"memory_usage_bytes":  paymentCount*200 + summaryCount*50,
	}
}
