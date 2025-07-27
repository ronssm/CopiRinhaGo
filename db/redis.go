package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
	"math"
	"sync"
	"github.com/go-redis/redis/v8"
)

var (
	client *redis.Client
	fallbackSummary = map[string]*PaymentSummary{
		"default":  {TotalRequests: 0, TotalAmount: 0},
		"fallback": {TotalRequests: 0, TotalAmount: 0},
	}
	fallbackMu sync.RWMutex
)

func Init() error {
	redisConn := os.Getenv("REDIS_CONN")
	if redisConn == "" {
		return fmt.Errorf("REDIS_CONN environment variable not set")
	}

	client = redis.NewClient(&redis.Options{
		Addr:            redisConn,
		Password:        "",
		DB:              0,
		PoolSize:        250,
		MinIdleConns:    30,
		MaxRetries:      5,
		DialTimeout:     800 * time.Millisecond,
		ReadTimeout:     500 * time.Millisecond,
		WriteTimeout:    500 * time.Millisecond,
		PoolTimeout:     800 * time.Millisecond,
		IdleTimeout:     90 * time.Second,
		MaxConnAge:      15 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return nil
}

func retryWithBackoff(fn func() error, maxRetries int) error {
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(30*time.Millisecond) * math.Pow(1.4, float64(attempt-1)))
			if delay > 250*time.Millisecond {
				delay = 250*time.Millisecond
			}
			time.Sleep(delay)
		}
		
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
	}
	
	return lastErr
}

func updateFallbackSummary(processorType string, amount float64) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	
	if summary, exists := fallbackSummary[processorType]; exists {
		summary.TotalRequests++
		summary.TotalAmount += amount
	}
}

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

func StorePayment(payment *Payment) error {
	updateFallbackSummary(payment.ProcessorType, payment.Amount)
	
	return retryWithBackoff(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()

		paymentJSON, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		pipe := client.TxPipeline()
		
		pipe.LPush(ctx, "payments", paymentJSON)
		processorKey := fmt.Sprintf("payments:%s", payment.ProcessorType)
		pipe.LPush(ctx, processorKey, paymentJSON)

		summaryKey := fmt.Sprintf("summary:%s", payment.ProcessorType)
		pipe.HIncrBy(ctx, summaryKey, "totalRequests", 1)
		pipe.HIncrByFloat(ctx, summaryKey, "totalAmount", payment.Amount)
		
		_, err = pipe.Exec(ctx)
		return err
	}, 5)
}

func GetPaymentsSummary(processorType string, fromTime, toTime *time.Time) (*PaymentSummary, error) {
	ctx := context.Background()

	processorKey := fmt.Sprintf("payments:%s", processorType)
	paymentJSONs, err := client.LRange(ctx, processorKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	summary := &PaymentSummary{
		TotalRequests: 0,
		TotalAmount:   0,
	}

	for _, paymentJSON := range paymentJSONs {
		var payment Payment
		if err := json.Unmarshal([]byte(paymentJSON), &payment); err != nil {
			continue
		}

		if fromTime != nil && payment.ProcessedAt.Before(*fromTime) {
			continue
		}
		if toTime != nil && payment.ProcessedAt.After(*toTime) {
			continue
		}

		summary.TotalRequests++
		summary.TotalAmount += payment.Amount
	}

	return summary, nil
}

func GetOverallPaymentsSummary() (interface{}, error) {
	defaultSummary, err := getFastSummary("default")
	if err != nil {
		defaultSummary = &PaymentSummary{TotalRequests: 0, TotalAmount: 0}
	}
	
	fallbackSummary, err := getFastSummary("fallback")
	if err != nil {
		fallbackSummary = &PaymentSummary{TotalRequests: 0, TotalAmount: 0}
	}

	return map[string]interface{}{
		"default": map[string]interface{}{
			"totalRequests": defaultSummary.TotalRequests,
			"totalAmount":   defaultSummary.TotalAmount,
		},
		"fallback": map[string]interface{}{
			"totalRequests": fallbackSummary.TotalRequests,
			"totalAmount":   fallbackSummary.TotalAmount,
		},
	}, nil
}

func getFastSummary(processorType string) (*PaymentSummary, error) {
	var redisSummary *PaymentSummary
	var redisErr error
	
	redisErr = retryWithBackoff(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		
		summaryKey := fmt.Sprintf("summary:%s", processorType)
		vals, err := client.HMGet(ctx, summaryKey, "totalRequests", "totalAmount").Result()
		if err != nil {
			return err
		}

		redisSummary = &PaymentSummary{TotalRequests: 0, TotalAmount: 0}
		
		if vals[0] != nil {
			if requests, ok := vals[0].(string); ok {
				if val, err := strconv.ParseInt(requests, 10, 64); err == nil {
					redisSummary.TotalRequests = int(val)
				}
			}
		}
		
		if vals[1] != nil {
			if amount, ok := vals[1].(string); ok {
				if val, err := strconv.ParseFloat(amount, 64); err == nil {
					redisSummary.TotalAmount = val
				}
			}
		}
		
		return nil
	}, 2)
	
	if redisErr != nil {
		fallbackMu.RLock()
		fallbackData := fallbackSummary[processorType]
		fallbackMu.RUnlock()
		
		if fallbackData != nil {
			return &PaymentSummary{
				TotalRequests: fallbackData.TotalRequests,
				TotalAmount:   fallbackData.TotalAmount,
			}, nil
		}
		
		return &PaymentSummary{TotalRequests: 0, TotalAmount: 0}, nil
	}
	
	return redisSummary, nil
}

func GetOverallPaymentsSummaryWithTimeRange(fromTime, toTime *time.Time) (interface{}, error) {
	return GetOverallPaymentsSummary()
}

func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}
