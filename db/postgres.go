package db

import (
    "database/sql"
    _ "github.com/lib/pq"
    "CopiRinhaGo/models"
)

var DB *sql.DB

func InitDB(connStr string) error {
    var err error
    DB, err = sql.Open("postgres", connStr)
    if err != nil {
        return err
    }
    return DB.Ping()
}

func InsertPayment(p *models.Payment) error {
    _, err := DB.Exec(`INSERT INTO payments (correlation_id, amount, processor, requested_at) VALUES ($1, $2, $3, $4)`,
        p.CorrelationID, p.Amount, p.Processor, p.RequestedAt)
    return err
}

type PaymentSummary struct {
    TotalRequests int     `json:"totalRequests"`
    TotalAmount   float64 `json:"totalAmount"`
}

func GetPaymentsSummary(from, to string) (map[string]PaymentSummary, error) {
    result := map[string]PaymentSummary{"default": {}, "fallback": {}}
    query := `SELECT processor, COUNT(*), COALESCE(SUM(amount),0) FROM payments WHERE 1=1`
    args := []interface{}{}
    if from != "" {
        query += " AND requested_at >= $1"
        args = append(args, from)
    }
    if to != "" {
        query += " AND requested_at <= $2"
        args = append(args, to)
    }
    query += " GROUP BY processor"
    rows, err := DB.Query(query, args...)
    if err != nil {
        return result, err
    }
    defer rows.Close()
    for rows.Next() {
        var proc string
        var count int
        var total float64
        if err := rows.Scan(&proc, &count, &total); err != nil {
            return result, err
        }
        result[proc] = PaymentSummary{TotalRequests: count, TotalAmount: total}
    }
    return result, nil
}
