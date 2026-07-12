-- Migration 000066: Replace ivfflat index with HNSW for better vector search performance
-- (issue: memory v2 HNSW index upgrade).
DO $$ BEGIN RAISE NOTICE '[Migration 000066] Replacing ivfflat index with HNSW (m=16, ef_construction=200)...'; END $$;

DROP INDEX IF EXISTS idx_agent_memories_embedding;

CREATE INDEX IF NOT EXISTS idx_agent_memories_embedding
  ON agent_memories USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 200);

DO $$ BEGIN RAISE NOTICE '[Migration 000066] HNSW index ready'; END $$;
