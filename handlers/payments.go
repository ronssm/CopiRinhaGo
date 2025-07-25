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
    "context"
    "log"
    "os"
    "strings"
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
    logLevel string
    paymentWorkerPool = make(chan struct{}, 32) // Limite de 32 goroutines
)

func getHealth(processor string) *HealthStatus {
    if logLevel == "" {
        logLevel = strings.ToUpper(os.Getenv("LOG_LEVEL"))
        if logLevel == "" {
            logLevel = "DEBUG"
        }
    }
    healthMu.Lock()
    defer healthMu.Unlock()
    status := healthCache[processor]
    if time.Since(status.LastChecked) < 3*time.Second {
        return status
    }
    url := ""
    if processor == "default" {
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][HEALTH] Verificando health do processor %s: %s", processor, url)
    }
        url = "http://payment-processor-default:8080/payments/service-health"
    } else {
        url = "http://payment-processor-fallback:8080/payments/service-health"
    }
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
        return status
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
        return status
    }
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][HEALTH] Cache hit para %s", processor)
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
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][FLOW] Escolhendo processor para pagamento...")
    }
    return "", errors.New("all processors failing")
}

func HandlePayment(w http.ResponseWriter, r *http.Request) {
    // Validação do payload
    if logLevel == "" {
        logLevel = strings.ToUpper(os.Getenv("LOG_LEVEL"))
        if logLevel == "" {
            logLevel = "DEBUG"
        }
    }
    var req struct {
        CorrelationID string  `json:"correlationId"`
        Amount        float64 `json:"amount"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }
    // Valida se correlationId é UUID
    if req.CorrelationID == "" || req.Amount <= 0 {
        http.Error(w, "missing or invalid fields", http.StatusBadRequest)
        return
    }
    if !isValidUUID(req.CorrelationID) {
        http.Error(w, "correlationId must be a valid UUID", http.StatusBadRequest)
        return
    }
    // Verifica unicidade do correlationId
    if paymentExists(req.CorrelationID) {
        http.Error(w, "correlationId already used", http.StatusConflict)
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
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    reqPost, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        http.Error(w, "Payment processor error", http.StatusServiceUnavailable)
        return
    }
    reqPost.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(reqPost)
    if err != nil || resp.StatusCode >= 500 {
        // Tenta fallback se default falhar
        if processor == "default" {
            url = "http://payment-processor-fallback:8080/payments"
            reqPost, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
            if err == nil {
                reqPost.Header.Set("Content-Type", "application/json")
                resp, err = http.DefaultClient.Do(reqPost)
                processor = "fallback"
            }
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
    // Processa pagamento em goroutine para não bloquear o handler
    go func(payment *models.Payment) {
        if err := db.InsertPayment(payment); err != nil {
            log.Printf("[ERROR][PAYMENT] Falha ao inserir pagamento: %v", err)
        } else if logLevel == "DEBUG" {
            log.Println("[DEBUG][PAYMENT] Pagamento processado com sucesso.")
        }
    }(payment)
    w.WriteHeader(http.StatusAccepted)
}

// Função para validar UUID
func isValidUUID(u string) bool {
    if len(u) != 36 {
        return false
    }
    // Regex simples para UUID v4
    for i, c := range u {
        switch i {
        case 8, 13, 18, 23:
            if c != '-' {
                return false
            }
        default:
            if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
                return false
            }
        }
    }
    return true
}

// Função para verificar se correlationId já existe
func paymentExists(correlationId string) bool {
    // Consulta simples no banco
    row := db.DB.QueryRow("SELECT 1 FROM payments WHERE correlation_id = $1", correlationId)
    var exists int
    err := row.Scan(&exists)
    return err == nil
}

