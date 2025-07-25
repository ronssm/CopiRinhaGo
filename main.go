package main

import (
    "log"
    "net/http"
    "os"
    "CopiRinhaGo/db"
    "CopiRinhaGo/handlers"
    "strings"
)

func main() {
    logLevel := strings.ToUpper(os.Getenv("LOG_LEVEL"))
    if logLevel == "" {
        logLevel = "DEBUG"
    }
    log.Printf("[INFO] LOG_LEVEL=%s", logLevel)

    debug := func(msg string) {
        if logLevel == "DEBUG" {
            log.Println(msg)
        }
    }
    info := func(msg string) {
        if logLevel == "DEBUG" || logLevel == "INFO" {
            log.Println(msg)
        }
    }
    errorLog := func(msg string) {
        log.Println(msg)
    }

    debug("[DEBUG] Iniciando backend CopiRinhaGo...")
    connStr := os.Getenv("POSTGRES_CONN")
    if connStr == "" {
        connStr = "postgres://user:password@db:5432/copirinha?sslmode=disable"
        debug("[DEBUG] Usando conexão padrão com o Postgres.")
    }
    debug("[DEBUG] Connecting to DB with: " + connStr)
    if err := db.InitDB(connStr); err != nil {
        errorLog("[ERROR] DB error: " + err.Error())
        os.Exit(1)
    }
    info("[INFO] Banco de dados conectado com sucesso.")
    http.HandleFunc("/payments", handlers.HandlePayment)
    info("[INFO] Rota /payments registrada.")
    http.HandleFunc("/payments-summary", handlers.HandlePaymentsSummary)
    info("[INFO] Rota /payments-summary registrada.")
    info("[INFO] Servidor iniciado na porta 9999.")
    log.Fatal(http.ListenAndServe(":9999", nil))
}
