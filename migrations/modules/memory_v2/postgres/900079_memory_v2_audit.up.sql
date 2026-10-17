-- Memory V2: per-memory audit trail.
-- Every user or worker mutation of a memory (create, patch, merge, delete,
-- dreamer action) records the actor and a before/after image. Powers the
-- History tab and makes merges reversible (undo restores the before images).
CREATE TABLE IF NOT EXISTS agent_memories_audit (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   VARCHAR(36) NOT NULL,
    kb_id       VARCHAR(36) NOT NULL DEFAULT '',
    memory_id   VARCHAR(36) NOT NULL,
    actor       VARCHAR(64) NOT NULL DEFAULT '',
    user_id     VARCHAR(36) NOT NULL DEFAULT '',
    action      VARCHAR(32) NOT NULL,
    before      JSONB,
    after       JSONB,
    details     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_memories_audit_scope
    ON agent_memories_audit(tenant_id, kb_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memories_audit_memory
    ON agent_memories_audit(memory_id, created_at DESC);
