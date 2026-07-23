-- Add verdict system to agent_memories and create dreamer_state table
-- (issue: memory v2 verdict/dreamer subsystem).
DO $$ BEGIN RAISE NOTICE '[Migration 000065] Adding verdict/hub_score columns to agent_memories, creating dreamer_state...'; END $$;

ALTER TABLE agent_memories
  ADD COLUMN IF NOT EXISTS verdict VARCHAR(16) DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS hub_score REAL DEFAULT 0;

CREATE TABLE IF NOT EXISTS dreamer_state (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id VARCHAR(36) NOT NULL UNIQUE,
    last_run_at TIMESTAMPTZ,
    locked_by VARCHAR(64),
    locked_until TIMESTAMPTZ,
    stats JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_memories_verdict
  ON agent_memories (tenant_id, verdict);

CREATE INDEX IF NOT EXISTS idx_agent_memories_session
  ON agent_memories (tenant_id, session_id);

DO $$ BEGIN RAISE NOTICE '[Migration 000065] Verdict columns and dreamer_state table ready'; END $$;
