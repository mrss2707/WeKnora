-- Development rollback only. Production rollback uses the worker kill switch.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema()
               AND table_name = 'extraction_queue' AND column_name = 'job_type') THEN
        DELETE FROM extraction_queue WHERE job_type = 'graph_link';
    END IF;
END $$;
DROP INDEX IF EXISTS idx_memory_v2_graph_job_unique;
DROP INDEX IF EXISTS idx_memory_v2_graph_job_ready;
ALTER TABLE IF EXISTS extraction_queue
    DROP COLUMN IF EXISTS job_type,
    DROP COLUMN IF EXISTS kb_id,
    DROP COLUMN IF EXISTS payload,
    DROP COLUMN IF EXISTS generation,
    DROP COLUMN IF EXISTS run_after,
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS locked_by;
