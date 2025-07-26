

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

var httpClient = &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        300,
        MaxIdleConnsPerHost: 150,
        IdleConnTimeout:     10 * time.Second,
        DisableKeepAlives:   false,
        DisableCompression:  true, // Reduz overhead
        ResponseHeaderTimeout: 500 * time.Millisecond,
    },
    Timeout: 800 * time.Millisecond, // Timeout agressivo para p99
}

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
    // Cache mais agressivo para melhor performance
    healthCacheDuration = 800 * time.Millisecond
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
    if time.Since(status.LastChecked) < healthCacheDuration {
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][HEALTH] Cache hit para %s", processor)
        }
        return status
    }
    url := ""
    if processor == "default" {
        url = "http://payment-processor-default:8080/payments"
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][HEALTH] Verificando health do processor %s: %s", processor, "http://payment-processor-default:8080/payments/service-health")
        }
    } else {
        url = "http://payment-processor-fallback:8080/payments/service-health"
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][HEALTH] Verificando health do processor %s: %s", processor, url)
        }
    }
    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
        return status
    }
    resp, err := httpClient.Do(req)
    if err != nil {
        status.Failing = true
        status.LastChecked = time.Now()
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
    resp.Body.Close()
    status.Failing = hs.Failing
    status.MinResponseTime = hs.MinResponseTime
    status.LastChecked = time.Now()
    return status
}

func chooseProcessor() (string, error) {
    def := getHealth("default")
    fb := getHealth("fallback")
    
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][FLOW] Health status - default: failing=%v, minTime=%d; fallback: failing=%v, minTime=%d", 
            def.Failing, def.MinResponseTime, fb.Failing, fb.MinResponseTime)
    }
    
    // Prioriza default se estiver funcionando (taxa menor)
    if !def.Failing {
        return "default", nil
    }
    // Se default falha, usa fallback se disponível
    if !fb.Failing {
        return "fallback", nil
    }
    // Se ambos falhando, ainda tenta default (pode ser instabilidade temporária)
    return "default", nil
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
    // Não verifica unicidade antes do insert, banco já garante
    processor, err := chooseProcessor()
    if err != nil {
        // Se chegou aqui, ambos estão falhando, mas ainda tenta default
        processor = "default"
        if logLevel == "DEBUG" {
            log.Println("[DEBUG][FLOW] Ambos processadores falhando, tentando default mesmo assim")
        }
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
    ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
    defer cancel()
    reqPost, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        http.Error(w, "Payment processor error", http.StatusServiceUnavailable)
        return
    }
    reqPost.Header.Set("Content-Type", "application/json")
    resp, err := httpClient.Do(reqPost)
    if err != nil || resp.StatusCode >= 500 {
        // Tenta fallback se default falhar
        if processor == "default" {
            if logLevel == "DEBUG" {
                log.Println("[DEBUG][FLOW] Default falhou, tentando fallback")
            }
            url = "http://payment-processor-fallback:8080/payments"
            reqPost, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
            if err == nil {
                reqPost.Header.Set("Content-Type", "application/json")
                resp, err = httpClient.Do(reqPost)
                if err == nil && resp.StatusCode < 500 {
                    processor = "fallback"
                }
            }
        }
    }
    
    // Se ainda há erro após tentativas, retorna erro HTTP 503
    if err != nil || resp.StatusCode >= 500 {
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][FLOW] Falha final em ambos processadores: err=%v, status=%d", err, 
                func() int { 
                    if resp != nil { 
                        return resp.StatusCode 
                    }
                    return 0 
                }())
        }
        http.Error(w, "Payment processor unavailable", http.StatusServiceUnavailable)
        return
    }
    payment := &models.Payment{
        CorrelationID: req.CorrelationID,
        Amount:        req.Amount,
        Processor:     processor,
        RequestedAt:   payload["requestedAt"].(string),
    }
    // Tenta inserir pagamento de forma síncrona para tratar erro de unicidade
    err = db.InsertPayment(payment)
    if err != nil {
        if errors.Is(err, db.ErrCorrelationIDExists) {
            http.Error(w, "correlationId already used", http.StatusConflict)
            return
        }
        log.Printf("[ERROR][PAYMENT] Falha ao inserir pagamento: %v", err)
        http.Error(w, "Erro ao registrar pagamento", http.StatusInternalServerError)
        return
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][PAYMENT] Pagamento processado com sucesso.")
    }
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

// Removido: unicidade garantida pelo banco

