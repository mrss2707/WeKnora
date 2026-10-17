-- Memory V2: reading a memory is not a change.
-- Recall/search record access (access_count, last_accessed_at). The
-- updated_at trigger from 900074 bumped updated_at on every UPDATE, so each
-- read looked like an edit: optimistic-lock checks failed for concurrent
-- editors and "inactivity" never accrued. Keep updated_at when only the
-- access columns change.
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
