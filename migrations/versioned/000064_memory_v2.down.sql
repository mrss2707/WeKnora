-- Rollback Memory v2 module (000064)
-- Drops all tables, indexes, and triggers created in the up migration

DROP TRIGGER IF EXISTS trg_agent_memories_updated_at ON agent_memories;
DROP FUNCTION IF EXISTS update_agent_memories_updated_at();

DROP TABLE IF EXISTS extraction_queue;
DROP TABLE IF EXISTS memory_relations;
DROP TABLE IF EXISTS agent_memories;
