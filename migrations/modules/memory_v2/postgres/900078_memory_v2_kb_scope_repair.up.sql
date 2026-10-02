-- Memory V2: audit log for the knowledge-base tenant-scope repair.
-- Before this change memories written through a shared knowledge base were
-- stored under the *caller's* tenant instead of the knowledge base owner's
-- tenant, which hid them from every other member. The application detects such
-- rows at startup and moves them to the owner tenant; each move is recorded
-- here so it can be audited or reverted.
CREATE TABLE IF NOT EXISTS agent_memories_scope_repair_log (
    id                  BIGSERIAL PRIMARY KEY,
    memory_id           VARCHAR(36) NOT NULL,
    kb_id               VARCHAR(36) NOT NULL,
    old_tenant_id       VARCHAR(36) NOT NULL,
    new_tenant_id       VARCHAR(36) NOT NULL,
    old_fingerprint     VARCHAR(64),
    new_fingerprint     VARCHAR(64),
    repaired_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memories_scope_repair_log_memory
    ON agent_memories_scope_repair_log(memory_id);
