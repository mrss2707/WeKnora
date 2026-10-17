-- Durable tenant-wide daily token reservations across KBs and app replicas.
CREATE TABLE IF NOT EXISTS memory_v2_dreamer_budget (
    reservation_id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    budget_day DATE NOT NULL,
    tokens BIGINT NOT NULL CHECK (tokens >= 0),
    settled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memory_v2_dreamer_budget_tenant_day
    ON memory_v2_dreamer_budget (tenant_id, budget_day);
