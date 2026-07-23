-- Migration 000064: Memory v2 module
-- Creates agent_memories, memory_relations, and extraction_queue tables
-- Uses PostgreSQL + pgvector + ParadeDB pg_search

-- ============================================================================
-- Table: agent_memories
-- Core memory storage with embedding, FTS, and tier-based lifecycle
-- ============================================================================
CREATE TABLE IF NOT EXISTS agent_memories (
    id              VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id       VARCHAR(36) NOT NULL,
    kb_id           VARCHAR(36) NOT NULL,
    user_id         VARCHAR(36) NOT NULL DEFAULT '',
    session_id      VARCHAR(36) NOT NULL DEFAULT '',

    content         TEXT NOT NULL,
    memory_type     VARCHAR(32) NOT NULL DEFAULT 'episodic',
    importance      INTEGER NOT NULL DEFAULT 0,
    tier            SMALLINT NOT NULL DEFAULT 1,

    -- Embedding vector (pgvector)
    embedding       vector(1536),

    -- Fingerprint for structural dedup (SHA256 first 200 normalized chars)
    fingerprint     VARCHAR(64),

    -- Tags and metadata
    tags            TEXT[] DEFAULT '{}',
    metadata        JSONB DEFAULT '{}',

    -- Access tracking
    access_count    BIGINT NOT NULL DEFAULT 0,
    last_accessed_at TIMESTAMP WITH TIME ZONE,

    -- Timestamps and soft delete
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMP WITH TIME ZONE,
    expires_at      TIMESTAMP WITH TIME ZONE
);

-- ============================================================================
-- Table: memory_relations
-- Graph edges linking memories for traversal and hub centrality
-- ============================================================================
CREATE TABLE IF NOT EXISTS memory_relations (
    id              VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id       VARCHAR(36) NOT NULL,
    from_uuid       VARCHAR(36) NOT NULL,
    to_uuid         VARCHAR(36) NOT NULL,
    relation_type   VARCHAR(64) NOT NULL,
    weight          REAL NOT NULL DEFAULT 1.0,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMP WITH TIME ZONE,

    CONSTRAINT fk_memory_relations_from
        FOREIGN KEY (from_uuid) REFERENCES agent_memories(id) ON DELETE CASCADE,
    CONSTRAINT fk_memory_relations_to
        FOREIGN KEY (to_uuid) REFERENCES agent_memories(id) ON DELETE CASCADE
);

-- ============================================================================
-- Table: extraction_queue
-- Async entity extraction job queue
-- ============================================================================
CREATE TABLE IF NOT EXISTS extraction_queue (
    id              VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id       VARCHAR(36) NOT NULL,
    memory_uuid     VARCHAR(36) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        SMALLINT NOT NULL DEFAULT 0,
    max_attempts    SMALLINT NOT NULL DEFAULT 3,
    error_message   TEXT,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_extraction_queue_memory
        FOREIGN KEY (memory_uuid) REFERENCES agent_memories(id) ON DELETE CASCADE
);

-- ============================================================================
-- Indexes: agent_memories
-- ============================================================================

-- B-tree composite indexes for filtering
CREATE INDEX IF NOT EXISTS idx_agent_memories_tenant_kb
    ON agent_memories(tenant_id, kb_id);

CREATE INDEX IF NOT EXISTS idx_agent_memories_tenant_kb_type
    ON agent_memories(tenant_id, kb_id, memory_type);

CREATE INDEX IF NOT EXISTS idx_agent_memories_user_id
    ON agent_memories(tenant_id, user_id);

CREATE INDEX IF NOT EXISTS idx_agent_memories_session_id
    ON agent_memories(tenant_id, session_id);

CREATE INDEX IF NOT EXISTS idx_agent_memories_expires_at
    ON agent_memories(tenant_id, kb_id, expires_at)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_memories_deleted_at
    ON agent_memories(deleted_at)
    WHERE deleted_at IS NOT NULL;

-- Fingerprint unique index for dedup
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_memories_fingerprint
    ON agent_memories(fingerprint)
    WHERE fingerprint IS NOT NULL AND deleted_at IS NULL;

-- GIN index for tags array
CREATE INDEX IF NOT EXISTS idx_agent_memories_tags
    ON agent_memories USING GIN(tags);

-- GIN tsvector index for full-text search (ParadeDB pg_search)
CREATE INDEX IF NOT EXISTS idx_agent_memories_fts
    ON agent_memories
    USING bm25 (id, tenant_id, kb_id, content, memory_type, importance, tier, tags)
    WITH (key_field='id');

-- ivfflat index for vector similarity search
CREATE INDEX IF NOT EXISTS idx_agent_memories_embedding
    ON agent_memories
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- ============================================================================
-- Indexes: memory_relations
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_memory_relations_tenant
    ON memory_relations(tenant_id);

CREATE INDEX IF NOT EXISTS idx_memory_relations_from
    ON memory_relations(from_uuid)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_memory_relations_to
    ON memory_relations(to_uuid)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_memory_relations_type
    ON memory_relations(relation_type)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_relations_unique
    ON memory_relations(from_uuid, to_uuid, relation_type)
    WHERE deleted_at IS NULL;

-- ============================================================================
-- Indexes: extraction_queue
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_extraction_queue_pending
    ON extraction_queue(status, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_extraction_queue_tenant
    ON extraction_queue(tenant_id);

CREATE INDEX IF NOT EXISTS idx_extraction_queue_memory
    ON extraction_queue(memory_uuid);

-- ============================================================================
-- Trigger: auto-update updated_at
-- ============================================================================
CREATE OR REPLACE FUNCTION update_agent_memories_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'trg_agent_memories_updated_at'
    ) THEN
        CREATE TRIGGER trg_agent_memories_updated_at
            BEFORE UPDATE ON agent_memories
            FOR EACH ROW
            EXECUTE FUNCTION update_agent_memories_updated_at();
    END IF;
END $$;

-- ============================================================================
-- Comments
-- ============================================================================
COMMENT ON TABLE agent_memories IS 'Core memory storage for Memory v2 module';
COMMENT ON COLUMN agent_memories.content IS 'Memory content text (10-10000 chars)';
COMMENT ON COLUMN agent_memories.memory_type IS 'episodic, semantic, procedural, decision, preference, fact';
COMMENT ON COLUMN agent_memories.importance IS 'Importance score (-5 to +6)';
COMMENT ON COLUMN agent_memories.tier IS 'Storage tier: 0=critical, 1=core, 2=standard, 3=edge';
COMMENT ON COLUMN agent_memories.fingerprint IS 'SHA256 of first 200 normalized chars for structural dedup';

COMMENT ON TABLE memory_relations IS 'Graph edges linking memories';
COMMENT ON COLUMN memory_relations.relation_type IS 'supports, contradicts, follows, justifies, co_tagged, related_to';

COMMENT ON TABLE extraction_queue IS 'Async entity extraction job queue';
