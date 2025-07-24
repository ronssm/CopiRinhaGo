package main

import (
    "log"
    "net/http"
    "os"
    "CopiRinhaGo/db"
    "CopiRinhaGo/handlers"
)

func main() {
    connStr := os.Getenv("POSTGRES_CONN")
    if connStr == "" {
        connStr = "postgres://user:password@db:5432/copirinha?sslmode=disable"
    }
    if err := db.InitDB(connStr); err != nil {
        log.Fatalf("DB error: %v", err)
    }
    http.HandleFunc("/payments", handlers.HandlePayment)
    http.HandleFunc("/payments-summary", handlers.HandlePaymentsSummary)
    log.Fatal(http.ListenAndServe(":9999", nil))
}
