DROP INDEX IF EXISTS idx_dreamer_state_scope;
ALTER TABLE dreamer_state DROP COLUMN IF EXISTS runs;
ALTER TABLE dreamer_state DROP COLUMN IF EXISTS heartbeat_at;
ALTER TABLE dreamer_state DROP COLUMN IF EXISTS kb_id;
-- Restore the 900080 trigger body (reads only).
CREATE OR REPLACE FUNCTION update_agent_memories_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.access_count IS DISTINCT FROM OLD.access_count
       AND (NEW.content, NEW.memory_type, NEW.importance, NEW.tier, NEW.verdict,
            NEW.tags, NEW.metadata, NEW.fingerprint, NEW.kb_id, NEW.tenant_id, NEW.deleted_at)
           IS NOT DISTINCT FROM
           (OLD.content, OLD.memory_type, OLD.importance, OLD.tier, OLD.verdict,
            OLD.tags, OLD.metadata, OLD.fingerprint, OLD.kb_id, OLD.tenant_id, OLD.deleted_at)
    THEN
        NEW.updated_at = OLD.updated_at;
    ELSE
        NEW.updated_at = CURRENT_TIMESTAMP;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
