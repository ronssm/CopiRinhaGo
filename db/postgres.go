package db

import (
    "database/sql"
    _ "github.com/lib/pq"
    "CopiRinhaGo/models"
    "log"
    "os"
    "strings"
    "errors"
    "strconv"
    "time"
)

var (
    DB *sql.DB
    logLevel string
)

func InitDB(connStr string) error {
    logLevel = strings.ToUpper(os.Getenv("LOG_LEVEL"))
    if logLevel == "" {
        logLevel = "INFO" // Recomenda INFO para produção
    }
    
    // Adiciona parâmetros de timeout à string de conexão
    if !strings.Contains(connStr, "?") {
        connStr += "?"
    } else {
        connStr += "&"
    }
    connStr += "statement_timeout=2000&lock_timeout=1000&idle_in_transaction_session_timeout=5000"
    
    var err error
    DB, err = sql.Open("postgres", connStr)
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao abrir conexão: %v", err)
        return err
    }
    
    // Pool otimizado para não exceder max_connections do PostgreSQL
    DB.SetMaxOpenConns(40)   // 40 por instância = 80 total (< 150 configurado)
    DB.SetMaxIdleConns(20)   // Metade do max_open
    DB.SetConnMaxLifetime(30 * time.Second) // 30 segundos
    DB.SetConnMaxIdleTime(10 * time.Second) // 10 segundos idle
    
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Pool de conexões ajustado: MaxOpenConns=40, MaxIdleConns=20, MaxLifetime=30s")
    }
    
    // Configura parâmetros por sessão para performance
    _, err = DB.Exec("SET work_mem = '4MB'")
    if err != nil {
        log.Printf("[WARN][DB] Não foi possível definir work_mem: %v", err)
    }
    
    _, err = DB.Exec("SET synchronous_commit = off")
    if err != nil {
        log.Printf("[WARN][DB] Não foi possível definir synchronous_commit: %v", err)
    }
    
    _, err = DB.Exec("SET random_page_cost = 1.1")
    if err != nil {
        log.Printf("[WARN][DB] Não foi possível definir random_page_cost: %v", err)
    }
    
    log.Println("[DEBUG][DB] Conexão aberta, testando ping...")
    if err := DB.Ping(); err != nil {
        log.Printf("[ERROR][DB] Falha no ping: %v", err)
        return err
    }
    log.Println("[DEBUG][DB] Ping bem-sucedido.")
    return nil
}

// Insere um pagamento na tabela, garantindo unicidade de correlationId
func InsertPayment(p *models.Payment) error {
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Inserindo pagamento: correlationId=%s, amount=%.2f, processor=%s, requestedAt=%s", p.CorrelationID, p.Amount, p.Processor, p.RequestedAt)
    }
    _, err := DB.Exec(`INSERT INTO payments (correlation_id, amount, processor, requested_at) VALUES ($1, $2, $3, $4)`,
        p.CorrelationID, p.Amount, p.Processor, p.RequestedAt)
    if err != nil {
        // Trata erro de violação de unicidade
        if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
            log.Printf("[ERROR][DB] correlationId já existe: %v", err)
            return ErrCorrelationIDExists
        }
        log.Printf("[ERROR][DB] Falha ao inserir pagamento: %v", err)
        return err
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][DB] Pagamento inserido com sucesso.")
    }
    return nil
}

// Erro customizado para violação de unicidade
var ErrCorrelationIDExists = errors.New("correlationId já existe")

type PaymentSummary struct {
    TotalRequests int     `json:"totalRequests"`
    TotalAmount   float64 `json:"totalAmount"`
}

// Retorna o resumo dos pagamentos agrupados por processor, com filtros opcionais de data
func GetPaymentsSummary(from, to string) (map[string]PaymentSummary, error) {
    if logLevel == "DEBUG" {
        log.Printf("[DEBUG][DB] Gerando resumo de pagamentos. Filtros: from=%s, to=%s", from, to)
    }
    result := map[string]PaymentSummary{"default": {}, "fallback": {}}
    query := `SELECT processor, COUNT(*), COALESCE(SUM(amount),0) FROM payments`
    args := []interface{}{}
    conds := []string{}
    
    if from != "" {
        conds = append(conds, "requested_at >= $"+strconv.Itoa(len(args)+1)+"::timestamptz")
        args = append(args, from)
    }
    if to != "" {
        conds = append(conds, "requested_at <= $"+strconv.Itoa(len(args)+1)+"::timestamptz")
        args = append(args, to)
    }
    if len(conds) > 0 {
        query += " WHERE " + strings.Join(conds, " AND ")
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
            continue
        }
        if logLevel == "DEBUG" {
            log.Printf("[DEBUG][DB] Resumo: processor=%s, totalRequests=%d, totalAmount=%.2f", proc, count, total)
        }
        // Garante que só "default" ou "fallback" sejam aceitos
        if proc == "default" || proc == "fallback" {
            result[proc] = PaymentSummary{TotalRequests: count, TotalAmount: total}
        }
    }
    
    if err := rows.Err(); err != nil {
        log.Printf("[ERROR][DB] Erro ao iterar resultados: %v", err)
        return result, err
    }
    
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][DB] Resumo de pagamentos gerado.")
    }
    return result, nil
}

// Reseta a tabela de pagamentos (truncate)
func ResetPayments() error {
    _, err := DB.Exec("TRUNCATE TABLE payments")
    if err != nil {
        log.Printf("[ERROR][DB] Falha ao resetar pagamentos: %v", err)
        return err
    }
    if logLevel == "DEBUG" {
        log.Println("[DEBUG][DB] Pagamentos resetados com sucesso.")
    }
    return nil
}
