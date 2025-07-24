package handlers

import (
    "encoding/json"
    "net/http"
    "sync"
    "time"
    "CopiRinhaGo/models"
    "CopiRinhaGo/db"
    "bytes"
    "errors"
)

type HealthStatus struct {
    Failing         bool
    MinResponseTime int
    LastChecked     time.Time
}

var (
    healthCache = map[string]*HealthStatus{
        "default":  &HealthStatus{LastChecked: time.Time{}},
        "fallback": &HealthStatus{LastChecked: time.Time{}},
    }
    healthMu sync.Mutex
)

func getHealth(processor string) *HealthStatus {
    healthMu.Lock()
    defer healthMu.Unlock()
    status := healthCache[processor]
    if time.Since(status.LastChecked) < 5*time.Second {
        return status
    }
    url := ""
    if processor == "default" {
        url = "http://payment-processor-default:8080/payments/service-health"
    } else {
        url = "http://payment-processor-fallback:8080/payments/service-health"
    }
    resp, err := http.Get(url)
    if err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
        return status
    }
    defer resp.Body.Close()
    if resp.StatusCode == 429 {
        return status
    }
    var hs struct {
        Failing         bool `json:"failing"`
        MinResponseTime int  `json:"minResponseTime"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&hs); err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
        return status
    }
    status.Failing = hs.Failing
    status.MinResponseTime = hs.MinResponseTime
    status.LastChecked = time.Now()
    return status
}

func chooseProcessor() (string, error) {
    def := getHealth("default")
    fb := getHealth("fallback")
    if !def.Failing {
        return "default", nil
    }
    if !fb.Failing {
        return "fallback", nil
    }
    return "", errors.New("all processors failing")
}

func HandlePayment(w http.ResponseWriter, r *http.Request) {
    var req struct {
        CorrelationID string  `json:"correlationId"`
        Amount        float64 `json:"amount"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }
    processor, err := chooseProcessor()
    if err != nil {
        http.Error(w, "No processors available", http.StatusServiceUnavailable)
        return
    }
    url := ""
    if processor == "default" {
        url = "http://payment-processor-default:8080/payments"
    } else {
        url = "http://payment-processor-fallback:8080/payments"
    }
    payload := map[string]interface{}{
        "correlationId": req.CorrelationID,
        "amount": req.Amount,
        "requestedAt": time.Now().UTC().Format(time.RFC3339Nano),
    }
    body, _ := json.Marshal(payload)
    resp, err := http.Post(url, "application/json", bytes.NewReader(body))
    if err != nil || resp.StatusCode >= 500 {
        // Try fallback if default failed
        if processor == "default" {
            url = "http://payment-processor-fallback:8080/payments"
            resp, err = http.Post(url, "application/json", bytes.NewReader(body))
            processor = "fallback"
        }
    }
    if err != nil || resp.StatusCode >= 500 {
        http.Error(w, "Payment processor error", http.StatusServiceUnavailable)
        return
    }
    payment := &models.Payment{
        CorrelationID: req.CorrelationID,
        Amount:        req.Amount,
        Processor:     processor,
        RequestedAt:   payload["requestedAt"].(string),
    }
    db.InsertPayment(payment)
    w.WriteHeader(http.StatusCreated)
}
