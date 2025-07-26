

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
        MaxIdleConns:        500,
        MaxIdleConnsPerHost: 200,
        IdleConnTimeout:     30 * time.Second,  // Increased from 5s
        DisableKeepAlives:   false,
        DisableCompression:  true, // Reduz overhead
        ResponseHeaderTimeout: 6 * time.Second, // Increased to handle slow fallback
    },
    Timeout: 7 * time.Second, // Increased to handle 5s fallback processor
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
    // Cache ajustado para respeitar limite de 1 chamada a cada 5 segundos
    healthCacheDuration = 5 * time.Second
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
        url = "http://payment-processor-default:8080/payments/service-health"
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][HEALTH] Verificando health do processor %s: %s", processor, url)
        }
    } else {
        url = "http://payment-processor-fallback:8080/payments/service-health"
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][HEALTH] Verificando health do processor %s: %s", processor, url)
        }
    }
    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond) // Increased from 100ms
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
    
    // Estratégia otimizada para cenários extremos de teste
    
    // REGRA 1: Evita processadores extremamente lentos (>2500ms) - Stage 05 fallback=5000ms
    if !def.Failing && fb.MinResponseTime > 2500 {
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][FLOW] Evitando fallback muito lento (%dms), usando default", fb.MinResponseTime)
        }
        return "default", nil
    }
    
    // REGRA 2: Se default OK e rápido (< 100ms), sempre usa default
    if !def.Failing && def.MinResponseTime < 100 {
        return "default", nil
    }
    
    // REGRA 3: Se default OK mas lento, usa fallback apenas se for significativamente mais rápido
    if !def.Failing {
        if !fb.Failing && fb.MinResponseTime < (def.MinResponseTime / 2) && fb.MinResponseTime < 2000 {
            return "fallback", nil
        }
        return "default", nil
    }
    
    // REGRA 4: Se default falha, usa fallback apenas se não for extremamente lento
    if !fb.Failing && fb.MinResponseTime < 2500 {
        return "fallback", nil
    }
    
    // REGRA 5: Em último caso, tenta default mesmo falhando (melhor que fallback muito lento)
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
    
    // Timeout adaptativo baseado no processador escolhido
    timeoutDuration := 6000 * time.Millisecond
    
    // Para fallback, verifica se está extremamente lento
    if processor == "fallback" {
        health := getHealth("fallback")
        if health.MinResponseTime > 2500 {
            // Timeout mais agressivo para fallback muito lento
            timeoutDuration = 3500 * time.Millisecond
            if logLevel == "DEBUG" {
                log.Printf("[DEBUG][FLOW] Usando timeout agressivo (3.5s) para fallback lento (%dms)", health.MinResponseTime)
            }
        }
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
    defer cancel()
    reqPost, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        http.Error(w, "Payment processor error", http.StatusServiceUnavailable)
        return
    }
    reqPost.Header.Set("Content-Type", "application/json")
    resp, err := httpClient.Do(reqPost)
    if err != nil || resp.StatusCode >= 500 {
        // Tenta fallback se default falhar (apenas uma vez)
        if processor == "default" {
            fallbackHealth := getHealth("fallback")
            
            // Só tenta fallback se não for extremamente lento
            if !fallbackHealth.Failing && fallbackHealth.MinResponseTime <= 2500 {
                if logLevel == "DEBUG" {
                    log.Printf("[DEBUG][FLOW] Default falhou, tentando fallback (tempo: %dms)", fallbackHealth.MinResponseTime)
                }
                
                // Timeout baseado na saúde do fallback
                fallbackTimeout := 6000 * time.Millisecond
                if fallbackHealth.MinResponseTime > 1500 {
                    fallbackTimeout = 3500 * time.Millisecond
                }
                
                ctxFallback, cancelFallback := context.WithTimeout(context.Background(), fallbackTimeout)
                defer cancelFallback()
                
                url = "http://payment-processor-fallback:8080/payments"
                reqPost, err = http.NewRequestWithContext(ctxFallback, "POST", url, bytes.NewReader(body))
                if err == nil {
                    reqPost.Header.Set("Content-Type", "application/json")
                    resp, err = httpClient.Do(reqPost)
                    if err == nil && resp.StatusCode < 500 {
                        processor = "fallback"
                    }
                }
            } else {
                if logLevel == "DEBUG" {
                    log.Printf("[DEBUG][FLOW] Fallback muito lento (%dms) ou falhando, não tentando", fallbackHealth.MinResponseTime)
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

