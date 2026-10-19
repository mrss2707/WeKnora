-- Per knowledge-base model choices for Memory V2 background work. A NULL model
-- keeps the knowledge base's summary model (then the tenant default).
CREATE TABLE IF NOT EXISTS memory_v2_scope_settings (
    tenant_id VARCHAR(36) NOT NULL,
    kb_id VARCHAR(36) NOT NULL,
    dreamer_model_id VARCHAR(64),
    text_model_id VARCHAR(64),
    updated_by VARCHAR(64) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, kb_id)
);
