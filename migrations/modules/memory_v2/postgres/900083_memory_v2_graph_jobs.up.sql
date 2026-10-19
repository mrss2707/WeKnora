-- Reuse the module's extraction queue for persistent graph-link recovery.
CREATE TABLE IF NOT EXISTS extraction_queue (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    memory_uuid VARCHAR(36) NOT NULL REFERENCES agent_memories(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts SMALLINT NOT NULL DEFAULT 0,
    max_attempts SMALLINT NOT NULL DEFAULT 3,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE extraction_queue
    ADD COLUMN IF NOT EXISTS job_type VARCHAR(32) NOT NULL DEFAULT 'entity',
    ADD COLUMN IF NOT EXISTS kb_id VARCHAR(36) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS generation BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS run_after TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS locked_by VARCHAR(64) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_v2_graph_job_unique
    ON extraction_queue (tenant_id, memory_uuid, job_type) WHERE job_type = 'graph_link';
CREATE INDEX IF NOT EXISTS idx_memory_v2_graph_job_ready
    ON extraction_queue (status, run_after, locked_until) WHERE job_type = 'graph_link';

-- Existing vectors with verified provenance can be linked without any model call.
-- Unknown legacy vectors are queued only after an explicit Admin re-embedding.
INSERT INTO extraction_queue (id, tenant_id, memory_uuid, kb_id, job_type, max_attempts)
SELECT gen_random_uuid()::text, tenant_id, id, kb_id, 'graph_link', 10
FROM agent_memories
WHERE deleted_at IS NULL AND kb_id <> '' AND embedding IS NOT NULL
  AND COALESCE(metadata->>'embedding_model_id', '') <> ''
ON CONFLICT (tenant_id, memory_uuid, job_type) WHERE job_type = 'graph_link' DO NOTHING;
