CREATE TABLE IF NOT EXISTS payments (
    id SERIAL PRIMARY KEY,
    correlation_id UUID NOT NULL,
    amount NUMERIC(18,2) NOT NULL,
    processor VARCHAR(16) NOT NULL,
    requested_at TIMESTAMP NOT NULL
);
