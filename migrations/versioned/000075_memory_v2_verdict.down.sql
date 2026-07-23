-- Revert migration 000065: remove verdict system from agent_memories and drop dreamer_state.
DO $$ BEGIN RAISE NOTICE '[Migration 000065] Reverting verdict/hub_score columns and dreamer_state...'; END $$;

ALTER TABLE agent_memories DROP COLUMN IF EXISTS verdict, DROP COLUMN IF EXISTS hub_score;

DROP TABLE IF EXISTS dreamer_state;

DROP INDEX IF EXISTS idx_agent_memories_verdict;
DROP INDEX IF EXISTS idx_agent_memories_session;

DO $$ BEGIN RAISE NOTICE '[Migration 000065] Revert complete'; END $$;
