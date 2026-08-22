DROP INDEX IF EXISTS idx_models_tenant_type_sort;
ALTER TABLE models DROP COLUMN IF EXISTS sort_order;
