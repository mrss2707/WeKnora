-- Revert migration 000066: restore ivfflat index, drop HNSW index.
DO $$ BEGIN RAISE NOTICE '[Migration 000066] Reverting HNSW index, restoring ivfflat...'; END $$;

DROP INDEX IF EXISTS idx_agent_memories_embedding;

CREATE INDEX IF NOT EXISTS idx_agent_memories_embedding
  ON agent_memories USING ivfflat (embedding vector_cosine_ops);

DO $$ BEGIN RAISE NOTICE '[Migration 000066] Revert complete'; END $$;
