CREATE UNLOGGED TABLE IF NOT EXISTS payments (
    id SERIAL PRIMARY KEY,
    correlation_id UUID NOT NULL,
    amount NUMERIC(18,2) NOT NULL,
    processor VARCHAR(16) NOT NULL,
    requested_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_correlation_id ON payments(correlation_id);
CREATE INDEX IF NOT EXISTS idx_processor ON payments(processor);
CREATE INDEX IF NOT EXISTS idx_requested_at ON payments(requested_at);
CREATE INDEX IF NOT EXISTS idx_correlation_processor ON payments(correlation_id, processor);
-- Garante autovacuum ativo
ALTER TABLE payments SET (autovacuum_enabled = true);
-- Garante unicidade do correlation_id
ALTER TABLE payments ADD CONSTRAINT unique_correlation_id UNIQUE (correlation_id);
