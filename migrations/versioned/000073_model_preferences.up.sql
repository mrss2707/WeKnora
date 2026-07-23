ALTER TABLE models ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_models_tenant_type_sort ON models(tenant_id, type, sort_order);
-- Backfill: preserve insertion order
UPDATE models SET sort_order = sub.rn
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id, type ORDER BY created_at) - 1 AS rn FROM models) sub
WHERE models.id = sub.id;
