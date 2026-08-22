# Model preferences module migration (PostgreSQL)

Adds per-tenant model ordering: `models.sort_order` plus index
`idx_models_tenant_type_sort`.

| Version | File | Purpose |
|---------|------|---------|
| 900073 | `model_preferences` | `ADD COLUMN IF NOT EXISTS sort_order`, index, backfill row numbers |

Renumbered to the reserved module range **900000–909999** because upstream
`main` uses core version 000073 for `kb_activity_scope`.

Every statement is idempotent; re-running on an existing database is safe. See
`migrations/modules/memory_v2/README.md` for the shared upgrade/rollback guide.