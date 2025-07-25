package handlers

import (
    "encoding/json"
    "net/http"
    "CopiRinhaGo/db"
    "log"
    "os"
    "strings"
)

func HandlePaymentsSummary(w http.ResponseWriter, r *http.Request) {
    logLevel := strings.ToUpper(os.Getenv("LOG_LEVEL"))
    if logLevel == "" {
        logLevel = "DEBUG"
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][API] Recebida chamada GET /payments-summary")
    }
    from := r.URL.Query().Get("from")
    to := r.URL.Query().Get("to")
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][API] Filtros recebidos: from=%s, to=%s", from, to)
    }
    summary, err := db.GetPaymentsSummary(from, to)
    if err != nil {
        log.Printf("[ERROR][API] Erro ao obter resumo: %v", err)
        http.Error(w, "summary error", http.StatusInternalServerError)
        return
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][API] Resumo obtido, enviando resposta.")
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(summary)
}
