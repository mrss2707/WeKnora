-- Memory V2: the dreamer runs per (tenant, knowledge base).
-- One lock/state row per scope instead of per tenant, so two knowledge bases
-- of a tenant are consolidated independently and a long pass on one does not
-- block the other. kb_id '' is the tenant-wide row used by older builds.
ALTER TABLE dreamer_state ADD COLUMN IF NOT EXISTS kb_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE dreamer_state ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;
ALTER TABLE dreamer_state ADD COLUMN IF NOT EXISTS runs INTEGER NOT NULL DEFAULT 0;

ALTER TABLE dreamer_state DROP CONSTRAINT IF EXISTS dreamer_state_tenant_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_dreamer_state_scope ON dreamer_state(tenant_id, kb_id);

-- Bookkeeping is not an edit: besides reads (900080), the dreamer marking a
-- memory as reviewed (metadata.dreamed_at) keeps updated_at, so it neither
-- breaks optimistic locks nor makes the scope look changed to the dreamer.
CREATE OR REPLACE FUNCTION update_agent_memories_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    IF (NEW.content, NEW.memory_type, NEW.importance, NEW.tier, NEW.verdict,
        NEW.tags, NEW.fingerprint, NEW.kb_id, NEW.tenant_id, NEW.deleted_at,
        COALESCE(NEW.metadata, '{}'::jsonb) - 'dreamed_at')
       IS NOT DISTINCT FROM
       (OLD.content, OLD.memory_type, OLD.importance, OLD.tier, OLD.verdict,
        OLD.tags, OLD.fingerprint, OLD.kb_id, OLD.tenant_id, OLD.deleted_at,
        COALESCE(OLD.metadata, '{}'::jsonb) - 'dreamed_at')
    THEN
        NEW.updated_at = OLD.updated_at;
    ELSE
        NEW.updated_at = CURRENT_TIMESTAMP;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
