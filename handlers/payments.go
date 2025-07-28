package handlers

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
    "sync"
    "context"
    "math"
    "github.com/gofiber/fiber/v2"
    "CopiRinhaGo/db"
)

var httpClient = &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:          800,
        MaxIdleConnsPerHost:   200,
        IdleConnTimeout:       90 * time.Second,
        DisableKeepAlives:     false,
        DisableCompression:    true,
        ResponseHeaderTimeout: 800 * time.Millisecond,
        TLSHandshakeTimeout:   500 * time.Millisecond,
        ExpectContinueTimeout: 200 * time.Millisecond,
        MaxConnsPerHost:       300,
        WriteBufferSize:       32768,
        ReadBufferSize:        32768,
        ForceAttemptHTTP2:     false,
    },
    Timeout: 2500 * time.Millisecond,
}

type PaymentRequest struct {
    CorrelationID string    `json:"correlationId"`
    Amount        float64   `json:"amount"`
}

type PaymentProcessorRequest struct {
    CorrelationID string    `json:"correlationId"`
    Amount        float64   `json:"amount"`
    RequestedAt   string    `json:"requestedAt"`
}

type HealthStatus struct {
    Failing         bool `json:"failing"`
    MinResponseTime int  `json:"minResponseTime"`
}

type HealthStatusWithTime struct {
    Failing         bool
    MinResponseTime int
    LastChecked     time.Time
}

var (
    healthCache = map[string]*HealthStatusWithTime{
        "default":  &HealthStatusWithTime{LastChecked: time.Time{}},
        "fallback": &HealthStatusWithTime{LastChecked: time.Time{}},
    }
    healthMu sync.RWMutex
    healthCacheDuration = 1500 * time.Millisecond
    
	circuitBreakers = map[string]*CircuitBreaker{
		"default":  NewCircuitBreaker(8, 10*time.Second),
		"fallback": NewCircuitBreaker(8, 10*time.Second),
	}
    processedRequests = make(map[string]time.Time)
    deduplicationMu sync.RWMutex
    deduplicationTTL = 5 * time.Minute
)

type CircuitBreaker struct {
    maxFailures   int
    resetTimeout  time.Duration
    mu           sync.RWMutex
    failures     int
    lastFailTime time.Time
    state        CircuitState
}

type CircuitState int

const (
    CircuitClosed CircuitState = iota
    CircuitOpen
    CircuitHalfOpen
)

func getHealth(processor string) *HealthStatusWithTime {
    healthMu.RLock()
    status := healthCache[processor]
    lastChecked := status.LastChecked
    healthMu.RUnlock()
    
    if time.Since(lastChecked) < healthCacheDuration {
        return status
    }
    
    go checkHealth(processor)
    return status
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
    return &CircuitBreaker{
        maxFailures:  maxFailures,
        resetTimeout: resetTimeout,
        state:       CircuitClosed,
    }
}

func (cb *CircuitBreaker) CanExecute() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    switch cb.state {
    case CircuitClosed:
        return true
    case CircuitOpen:
        if time.Since(cb.lastFailTime) > cb.resetTimeout {
            cb.state = CircuitHalfOpen
            return true
        }
        return false
    case CircuitHalfOpen:
        return true
    default:
        return false
    }
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    cb.failures = 0
    cb.state = CircuitClosed
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    cb.failures++
    cb.lastFailTime = time.Now()
    
    if cb.state == CircuitHalfOpen {
        cb.state = CircuitOpen
    } else if cb.failures >= cb.maxFailures {
        cb.state = CircuitOpen
    }
}

func checkHealth(processor string) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    url := fmt.Sprintf("http://payment-processor-%s:8080/payments/service-health", processor)
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        healthMu.Lock()
        status := healthCache[processor]
        status.LastChecked = time.Now()
        status.Failing = true
        status.MinResponseTime = 9999
        healthMu.Unlock()
        return
    }
    
    resp, err := httpClient.Do(req)
    
    healthMu.Lock()
    defer healthMu.Unlock()
    
    status := healthCache[processor]
    status.LastChecked = time.Now()
    
    if err != nil || resp.StatusCode != 200 {
        status.Failing = true
        status.MinResponseTime = 9999
    } else {
        var healthResp HealthStatus
        if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
            status.Failing = true
            status.MinResponseTime = 9999
        } else {
            status.Failing = healthResp.Failing
            status.MinResponseTime = healthResp.MinResponseTime
        }
        resp.Body.Close()
    }
}

func selectProcessor() string {
    defaultHealth := getHealth("default")
    fallbackHealth := getHealth("fallback")
    
    defaultCB := circuitBreakers["default"]
    fallbackCB := circuitBreakers["fallback"]
    
    defaultAvailable := !defaultHealth.Failing && defaultCB.CanExecute()
    fallbackAvailable := !fallbackHealth.Failing && fallbackCB.CanExecute()
    
    if defaultAvailable {
        if fallbackAvailable && defaultHealth.MinResponseTime > fallbackHealth.MinResponseTime+100 {
            return "fallback"
        }
        return "default"
    }
    
    if fallbackAvailable {
        return "fallback"
    }
    
    return "default"
}

func isDuplicateRequest(correlationID string) bool {
    deduplicationMu.RLock()
    lastProcessed, exists := processedRequests[correlationID]
    deduplicationMu.RUnlock()
    
    if exists && time.Since(lastProcessed) < deduplicationTTL {
        return true
    }
    
    go cleanupDeduplicationCache()
    
    return false
}

func markRequestProcessed(correlationID string) {
    deduplicationMu.Lock()
    processedRequests[correlationID] = time.Now()
    deduplicationMu.Unlock()
}

func cleanupDeduplicationCache() {
    deduplicationMu.Lock()
    defer deduplicationMu.Unlock()
    
    cutoff := time.Now().Add(-deduplicationTTL)
    for id, timestamp := range processedRequests {
        if timestamp.Before(cutoff) {
            delete(processedRequests, id)
        }
    }
}

func retryWithExponentialBackoff(fn func() error, maxRetries int, initialDelay time.Duration) error {
    var lastErr error
    
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            delay := time.Duration(float64(initialDelay) * math.Pow(1.3, float64(attempt-1)))
            if delay > 150*time.Millisecond {
                delay = 150 * time.Millisecond
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

func processPayment(correlationID string, amount float64) (string, error) {
    if correlationID == "" {
        return "", fmt.Errorf("correlationId is required")
    }
    
    if isDuplicateRequest(correlationID) {
        return "duplicate", fmt.Errorf("duplicate request for correlationId: %s", correlationID)
    }
    
    var lastErr error
    var processorTried []string
    
    preferredOrder := []string{selectProcessor()}
    if preferredOrder[0] == "default" {
        preferredOrder = append(preferredOrder, "fallback")
    } else {
        preferredOrder = append(preferredOrder, "default")
    }
    
    for _, processorType := range preferredOrder {
        cb := circuitBreakers[processorType]
        
        if !cb.CanExecute() {
            lastErr = fmt.Errorf("circuit breaker open for processor: %s", processorType)
            processorTried = append(processorTried, processorType+" (circuit-open)")
            continue
        }
        
        err := retryWithExponentialBackoff(func() error {
            return attemptPaymentProcessing(correlationID, amount, processorType)
        }, 1, 15*time.Millisecond)
        
        if err == nil {
            cb.RecordSuccess()
            
            payment := &db.Payment{
                CorrelationID: correlationID,
                Amount:        amount,
                ProcessorType: processorType,
                ProcessedAt:   time.Now(),
            }
            
            storageErr := retryWithExponentialBackoff(func() error {
                return db.StorePayment(payment)
            }, 2, 5*time.Millisecond)
            
            if storageErr != nil {
                return "", fmt.Errorf("payment processed but storage failed: %v", storageErr)
            }
            
            markRequestProcessed(correlationID)
            
            return processorType, nil
        }
        
        cb.RecordFailure()
        lastErr = err
        processorTried = append(processorTried, processorType)
    }
    
    return "", fmt.Errorf("all payment processors failed (%v): %v", processorTried, lastErr)
}

func attemptPaymentProcessing(correlationID string, amount float64, processorType string) error {
    paymentData := PaymentProcessorRequest{
        CorrelationID: correlationID,
        Amount:        amount,
        RequestedAt:   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
    }
    
    jsonData, err := json.Marshal(paymentData)
    if err != nil {
        return fmt.Errorf("json marshal error: %v", err)
    }
    
    url := fmt.Sprintf("http://payment-processor-%s:8080/payments", processorType)
    
    ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Millisecond)
    defer cancel()
    
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return fmt.Errorf("request creation error: %v", err)
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("User-Agent", "CopiRinhaGo/1.0")
    
    resp, err := httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("payment processing failed: %v", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("payment processing error: status %d", resp.StatusCode)
    }
    
    return nil
}

func HandlePaymentFiber(c *fiber.Ctx) error {
    var paymentRequest PaymentRequest
    
    if err := c.BodyParser(&paymentRequest); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid json"})
    }

    if paymentRequest.CorrelationID == "" {
        return c.Status(400).JSON(fiber.Map{"error": "correlationId is required"})
    }

    if paymentRequest.Amount <= 0 {
        return c.Status(400).JSON(fiber.Map{"error": "amount must be positive"})
    }
    
    if paymentRequest.Amount > 1000000 {
        return c.Status(400).JSON(fiber.Map{"error": "amount exceeds maximum limit"})
    }

    processorType, err := processPayment(paymentRequest.CorrelationID, paymentRequest.Amount)
    if err != nil {
        if processorType == "duplicate" {
            return c.Status(409).JSON(fiber.Map{"error": "duplicate correlationId"})
        }
        
        return c.Status(500).JSON(fiber.Map{"error": "payment processing failed"})
    }

    return c.Status(200).JSON(fiber.Map{"message": "payment processed successfully"})
}
