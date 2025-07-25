package db

import (
    "database/sql"
    _ "github.com/lib/pq"
    "CopiRinhaGo/models"
    "log"
    "os"
    "strings"
)

var (
    DB *sql.DB
    logLevel string
    summaryCache struct {
        data map[string]PaymentSummary
        expiresAt int64
        from, to string
    }
)

func InitDB(connStr string) error {
    logLevel = strings.ToUpper(os.Getenv("LOG_LEVEL"))
    if logLevel == "" {
        logLevel = "INFO" // Recomenda INFO para produção
    }
    var err error
    DB, err = sql.Open("postgres", connStr)
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao abrir conexão: %v", err)
        return err
    }
    // Otimização do pool de conexões
    DB.SetMaxOpenConns(50)
    DB.SetMaxIdleConns(25)
    DB.SetConnMaxLifetime(300000000000) // 5 minutos
    log.Println("[DEBUG][DB] Pool de conexões ajustado: MaxOpenConns=10, MaxIdleConns=5, MaxLifetime=5min.")
    log.Println("[DEBUG][DB] Conexão aberta, testando ping...")
    if err := DB.Ping(); err != nil {
        log.Printf("[ERROR][DB] Falha no ping: %v", err)
        return err
    }
    log.Println("[DEBUG][DB] Ping bem-sucedido.")
    return nil
}

func InsertPayment(p *models.Payment) error {
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Inserindo pagamento: correlationId=%s, amount=%.2f, processor=%s, requestedAt=%s", p.CorrelationID, p.Amount, p.Processor, p.RequestedAt)
    }
    stmt, err := DB.Prepare(`INSERT INTO payments (correlation_id, amount, processor, requested_at) VALUES ($1, $2, $3, $4)`)
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao preparar statement: %v", err)
        return err
    }
    defer stmt.Close()
    _, err = stmt.Exec(p.CorrelationID, p.Amount, p.Processor, p.RequestedAt)
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao inserir pagamento: %v", err)
    } else if logLevel == "DEBUG" {
        log.Println("[DEBUG][DB] Pagamento inserido com sucesso.")
    }
    return err
}

type PaymentSummary struct {
    TotalRequests int     `json:"totalRequests"`
    TotalAmount   float64 `json:"totalAmount"`
}

func GetPaymentsSummary(from, to string) (map[string]PaymentSummary, error) {
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Gerando resumo de pagamentos. Filtros: from=%s, to=%s", from, to)
    }
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
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Executando query: %s, args=%v", query, args)
    }
    rows, err := DB.Query(query, args...)
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao consultar resumo: %v", err)
        return result, err
    }
    defer rows.Close()
    for rows.Next() {
        var proc string
        var count int
        var total float64
        if err := rows.Scan(&proc, &count, &total); err != nil {
            log.Printf("[ERROR][DB] Falha ao ler linha do resumo: %v", err)
    summaryCache.data = result
    summaryCache.expiresAt = now + 2
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][DB] Resumo: processor=%s, totalRequests=%d, totalAmount=%.2f", proc, count, total)
        }
        result[proc] = PaymentSummary{TotalRequests: count, TotalAmount: total}
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][DB] Resumo de pagamentos gerado.")
    }
    return result, nil
}
