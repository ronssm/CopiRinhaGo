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
        MaxIdleConns:          1200,
        MaxIdleConnsPerHost:   300,
        IdleConnTimeout:       60 * time.Second,
        DisableKeepAlives:     false,
        DisableCompression:    true,
        ResponseHeaderTimeout: 1500 * time.Millisecond,
        TLSHandshakeTimeout:   500 * time.Millisecond,
        ExpectContinueTimeout: 200 * time.Millisecond,
        MaxConnsPerHost:       400,
        WriteBufferSize:       65536,
        ReadBufferSize:        65536,
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

type ProcessorStats struct {
    SuccessCount     int64
    FailureCount     int64
    AvgResponseTime  float64
    LastSuccess      time.Time
    ConsecutiveFails int
}

var (
    healthCache = map[string]*HealthStatusWithTime{
        "default":  &HealthStatusWithTime{LastChecked: time.Time{}, Failing: false, MinResponseTime: 50},
        "fallback": &HealthStatusWithTime{LastChecked: time.Time{}, Failing: false, MinResponseTime: 100},
    }
    healthMu sync.RWMutex
    healthCacheDuration = 3000 * time.Millisecond

	circuitBreakers = map[string]*CircuitBreaker{
		"default":  NewCircuitBreaker(8, 10*time.Second),
		"fallback": NewCircuitBreaker(8, 10*time.Second),
	}
    processedRequests = make(map[string]time.Time)
    deduplicationMu sync.RWMutex
    deduplicationTTL = 10 * time.Minute


    processorPerformance = map[string]*ProcessorStats{
        "default":  &ProcessorStats{},
        "fallback": &ProcessorStats{},
    }
    performanceMu sync.RWMutex


    batchQueue = make(chan *BatchRequest, 5000)
    batchPool sync.Pool
    responsePool sync.Pool


    requestWorkerPool = make(chan *WorkerRequest, 1000)
    dbStorageQueue = make(chan *db.Payment, 500)
    healthCheckTicker = time.NewTicker(200 * time.Millisecond)
    cleanupTicker = time.NewTicker(15 * time.Second)
)

type BatchRequest struct {
    CorrelationID string
    Amount        float64
    ResponseChan  chan BatchResponse
    Timestamp     time.Time
}

type BatchResponse struct {
    ProcessorType string
    Error         error
}

type WorkerRequest struct {
    BatchReq  *BatchRequest
    Processor string
}

func init() {

    batchPool.New = func() interface{} {
        return &BatchRequest{}
    }

    responsePool.New = func() interface{} {
        return make(chan BatchResponse, 1)
    }


    for i := 0; i < 8; i++ {
        go batchProcessor()
    }



    for i := 0; i < 16; i++ {
        go requestWorker()
    }


    go dbBatchWorker()


    go healthCheckWorker()


    go cleanupWorker()
}

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

func updateProcessorStats(processorType string, success bool, responseTime time.Duration) {
    performanceMu.Lock()
    defer performanceMu.Unlock()

    stats := processorPerformance[processorType]

    if success {
        stats.SuccessCount++
        stats.ConsecutiveFails = 0
        stats.LastSuccess = time.Now()


        if stats.AvgResponseTime == 0 {
            stats.AvgResponseTime = float64(responseTime.Nanoseconds())
        } else {
            stats.AvgResponseTime = 0.7*stats.AvgResponseTime + 0.3*float64(responseTime.Nanoseconds())
        }
    } else {
        stats.FailureCount++
        stats.ConsecutiveFails++
    }
}

func checkHealth(processor string) {
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    url := fmt.Sprintf("http://payment-processor-%s:8080/payments/service-health", processor)
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        updateHealthStatus(processor, true, 9999)
        return
    }

    resp, err := httpClient.Do(req)

    if err != nil || resp == nil || resp.StatusCode != 200 {
        updateHealthStatus(processor, true, 9999)
        return
    }
    defer resp.Body.Close()

    var healthResp HealthStatus
    if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
        updateHealthStatus(processor, true, 9999)
    } else {
        updateHealthStatus(processor, healthResp.Failing, healthResp.MinResponseTime)
    }
}

func updateHealthStatus(processor string, failing bool, minResponseTime int) {
    healthMu.Lock()
    defer healthMu.Unlock()

    status := healthCache[processor]
    status.LastChecked = time.Now()
    status.Failing = failing
    status.MinResponseTime = minResponseTime
}

func selectProcessor() string {
    defaultHealth := getHealth("default")
    fallbackHealth := getHealth("fallback")

    defaultCB := circuitBreakers["default"]
    fallbackCB := circuitBreakers["fallback"]


    performanceMu.RLock()
    defaultStats := processorPerformance["default"]
    fallbackStats := processorPerformance["fallback"]
    performanceMu.RUnlock()


    defaultScore := calculateProcessorScore("default", defaultHealth, defaultCB, defaultStats)
    fallbackScore := calculateProcessorScore("fallback", fallbackHealth, fallbackCB, fallbackStats)


    if defaultScore > 0 && (defaultScore >= fallbackScore || fallbackScore == 0) {
        return "default"
    }

    if fallbackScore > 0 {
        return "fallback"
    }


    return "default"
}


func calculateProcessorScore(processor string, health *HealthStatusWithTime, cb *CircuitBreaker, stats *ProcessorStats) float64 {
    if health.Failing {
        return 0
    }

    if !cb.CanExecute() {
        return 0
    }


    score := 1.0


    if stats.ConsecutiveFails > 0 {
        score *= math.Pow(0.5, float64(stats.ConsecutiveFails))
    }


    if time.Since(stats.LastSuccess) < 30*time.Second {
        score *= 1.5
    }


    if stats.AvgResponseTime > 0 {
        responseTimeMs := stats.AvgResponseTime / 1000000
        if responseTimeMs > 1000 {
            score *= 0.5
        }
    }

    return score
}

func isDuplicateRequest(correlationID string) bool {
    deduplicationMu.RLock()
    lastProcessed, exists := processedRequests[correlationID]
    deduplicationMu.RUnlock()

    if exists && time.Since(lastProcessed) < deduplicationTTL {
        return true
    }


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
            delay := time.Duration(float64(initialDelay) * math.Pow(1.2, float64(attempt-1)))
            if delay > 200*time.Millisecond {
                delay = 200 * time.Millisecond
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

func batchProcessor() {
    batch := make([]*BatchRequest, 0, 20)
    ticker := time.NewTicker(5 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case req := <-batchQueue:
            batch = append(batch, req)


            if len(batch) >= 15 || (len(batch) == 1 && time.Since(req.Timestamp) > 10*time.Millisecond) {
                processBatch(batch)
                batch = batch[:0]
            }

        case <-ticker.C:
            if len(batch) > 0 {
                processBatch(batch)
                batch = batch[:0]
            }
        }
    }
}

func processBatch(batch []*BatchRequest) {
    if len(batch) == 0 {
        return
    }


    processor := selectProcessor()


    for _, req := range batch {
        workerReq := &WorkerRequest{
            BatchReq:  req,
            Processor: processor,
        }

        select {
        case requestWorkerPool <- workerReq:

        default:

            go processSingleRequest(req, processor)
        }
    }
}

func processSingleRequest(req *BatchRequest, processor string) {
    defer func() {

        responsePool.Put(req.ResponseChan)
        batchPool.Put(req)
    }()

    processorType, err := attemptPaymentWithBalancedRetry(req.CorrelationID, req.Amount, processor)

    select {
    case req.ResponseChan <- BatchResponse{ProcessorType: processorType, Error: err}:
    default:

    }
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}

func processPayment(correlationID string, amount float64) (string, error) {
    if correlationID == "" {
        return "", fmt.Errorf("correlationId is required")
    }

    if isDuplicateRequest(correlationID) {
        return "duplicate", fmt.Errorf("duplicate request for correlationId: %s", correlationID)
    }


    req := batchPool.Get().(*BatchRequest)
    req.CorrelationID = correlationID
    req.Amount = amount
    req.ResponseChan = responsePool.Get().(chan BatchResponse)
    req.Timestamp = time.Now()


    select {
    case batchQueue <- req:

        select {
        case response := <-req.ResponseChan:
            if response.Error == nil {

                payment := &db.Payment{
                    CorrelationID: correlationID,
                    Amount:        amount,
                    ProcessorType: response.ProcessorType,
                    ProcessedAt:   time.Now(),
                }

                select {
                case dbStorageQueue <- payment:

                default:

                    go func(p *db.Payment) {
                        retryWithExponentialBackoff(func() error {
                            return db.StorePayment(p)
                        }, 2, 5*time.Millisecond)
                    }(payment)
                }

                markRequestProcessed(correlationID)
                return response.ProcessorType, nil
            }
            return "", response.Error

        case <-time.After(4 * time.Second):
            return "", fmt.Errorf("batch processing timeout")
        }

    default:

        responsePool.Put(req.ResponseChan)
        batchPool.Put(req)

        return processPaymentDirect(correlationID, amount)
    }
}

func processPaymentDirect(correlationID string, amount float64) (string, error) {

    primaryProcessor := selectProcessor()


    processorType, err := attemptPaymentWithBalancedRetry(correlationID, amount, primaryProcessor)
    if err == nil {

        payment := &db.Payment{
            CorrelationID: correlationID,
            Amount:        amount,
            ProcessorType: processorType,
            ProcessedAt:   time.Now(),
        }

        select {
        case dbStorageQueue <- payment:

        default:

            go func(p *db.Payment) {
                retryWithExponentialBackoff(func() error {
                    return db.StorePayment(p)
                }, 2, 5*time.Millisecond)
            }(payment)
        }

        markRequestProcessed(correlationID)
        return processorType, nil
    }

    return "", err
}

func attemptPaymentWithBalancedRetry(correlationID string, amount float64, primaryProcessor string) (string, error) {

    for attempt := 0; attempt < 2; attempt++ {
        if attempt > 0 {
            time.Sleep(20 * time.Millisecond)
        }

        if err := attemptPaymentProcessing(correlationID, amount, primaryProcessor); err == nil {
            circuitBreakers[primaryProcessor].RecordSuccess()
            return primaryProcessor, nil
        } else if attempt == 0 {

            continue
        } else {
            circuitBreakers[primaryProcessor].RecordFailure()
        }
    }


    fallbackProcessor := "fallback"
    if primaryProcessor == "fallback" {
        fallbackProcessor = "default"
    }


    fallbackCB := circuitBreakers[fallbackProcessor]
    if fallbackCB.CanExecute() {
        if err := attemptPaymentProcessing(correlationID, amount, fallbackProcessor); err == nil {
            circuitBreakers[fallbackProcessor].RecordSuccess()
            return fallbackProcessor, nil
        } else {
            circuitBreakers[fallbackProcessor].RecordFailure()
        }
    }

    return "", fmt.Errorf("both processors failed")
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

    ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return fmt.Errorf("request creation error: %v", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Connection", "keep-alive")

    start := time.Now()
    resp, err := httpClient.Do(req)
    responseTime := time.Since(start)


    success := err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
    updateProcessorStats(processorType, success, responseTime)

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
        return c.Status(400).JSON(fiber.Map{"error": "correlationId required"})
    }

    if paymentRequest.Amount <= 0 || paymentRequest.Amount > 1000000 {
        return c.Status(400).JSON(fiber.Map{"error": "invalid amount"})
    }

    processorType, err := processPayment(paymentRequest.CorrelationID, paymentRequest.Amount)
    if err != nil {
        if processorType == "duplicate" {

            return c.Status(200).JSON(fiber.Map{"message": "processed"})
        }

        return c.Status(500).JSON(fiber.Map{"error": "processing failed"})
    }


    return c.Status(201).SendString(`{"message":"processed"}`)
}




func requestWorker() {
    for workerReq := range requestWorkerPool {
        processSingleRequest(workerReq.BatchReq, workerReq.Processor)
    }
}


func dbBatchWorker() {
    batch := make([]*db.Payment, 0, 10)
    ticker := time.NewTicker(50 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case payment := <-dbStorageQueue:
            batch = append(batch, payment)


            if len(batch) >= 5 {
                storeBatch(batch)
                batch = batch[:0]
            }

        case <-ticker.C:
            if len(batch) > 0 {
                storeBatch(batch)
                batch = batch[:0]
            }
        }
    }
}


func storeBatch(payments []*db.Payment) {
    for _, payment := range payments {

        retryWithExponentialBackoff(func() error {
            return db.StorePayment(payment)
        }, 2, 5*time.Millisecond)
    }
}


func healthCheckManager() {
    ticker := time.NewTicker(1500 * time.Millisecond)
    defer ticker.Stop()

    processors := []string{"default", "fallback"}

    for {
        select {
        case <-ticker.C:
            for _, processor := range processors {
                healthMu.RLock()
                lastChecked := healthCache[processor].LastChecked
                healthMu.RUnlock()

                if time.Since(lastChecked) >= healthCacheDuration {
                    checkHealth(processor)
                }
            }
        }
    }
}


func cleanupManager() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            cleanupDeduplicationCache()
        }
    }
}


func healthCheckWorker() {
    for {
        select {
        case <-healthCheckTicker.C:

            go checkHealth("default")
            go checkHealth("fallback")
        }
    }
}


func cleanupWorker() {
    for {
        select {
        case <-cleanupTicker.C:
            cleanupDeduplicationCache()
        }
    }
}
