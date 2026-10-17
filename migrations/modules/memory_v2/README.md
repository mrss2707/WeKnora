# Memory V2 module migrations (PostgreSQL)

Native Go + PostgreSQL/pgvector memory module (per-KB, multi-tenant). Schema:
`agent_memories`, `memory_relations`, `extraction_queue`, `dreamer_state`.

## Layout & numbering

| Version | File | Purpose |
|---------|------|---------|
| 900074 | `memory_v2` | Creates `agent_memories` (hybrid vector + BM25 search, tiers), `memory_relations`, `extraction_queue` |
| 900075 | `memory_v2_verdict` | Verdict system on `agent_memories` + `dreamer_state` |
| 900076 | `memory_v2_hnsw` | Replaces ivfflat with HNSW vector index |
| 900078 | `memory_v2_kb_scope_repair` | Audit log for moving shared-KB memories to the KB owner's tenant |
| 900079 | `memory_v2_audit` | Per-memory audit trail (actor, before/after) for History and merge undo |
| 900080 | `memory_v2_access_trigger` | Reads (access_count) no longer bump updated_at |
| 900081 | `memory_v2_dreamer_scope` | Dreamer lock/state per (tenant, knowledge base) with heartbeat |
| 900082 | `memory_v2_dreamer_budget` | Durable UTC daily token reservations per tenant across KBs and app replicas |

Dreamer reserves its maximum estimated prompt/output budget before calling the model.
Successful calls settle once to their estimated usage. Failed/uncertain calls and
crashes leave the reservation charged for that UTC day; restarting an app does not
reset the cap. This is an **estimated-token** budget, not a provider billing guarantee.

Module migrations live in the reserved range **900000–909999** so they can never
collide with core migrations in `migrations/versioned/` (which upstream `main`
advances). All files are assembled by the single composite migration source
(`internal/database/migration_source.go`) together with core migrations, in
ascending order, with duplicate-version rejection.

## Prerequisites

- PostgreSQL (13+; `gen_random_uuid()` is built-in)
- `vector` extension from **pgvector** (vector type, `vector_cosine_ops`)
- **ParadeDB pg_search** extension (bm25 full-text index)

## Backend scoping

These files apply to the `postgres` backend only. SQLite/Lite mode never reads
this directory (Lite reads `migrations/modules/<name>/sqlite/` and
`migrations/sqlite/`, each with its own version table — verify with
`./scripts/migrate.sh validate sqlite`).

## Runtime default

Memory V2 is the default runtime. The legacy cross-session memory engine from
`main` is registered only when the `cross_session_memory` config flag is
enabled, inside one gated block (`internal/container/cross_session_memory.go`).
Data conversion between the two engines is **out of scope**.

## Embedding dimensions

The `agent_memories.embedding` column is declared `vector(2000)`: pgvector
rejects ivfflat/hnsw indexes on columns wider than 2000 dims, so 2000 is the
supported maximum. Any embedding model with **≤ 2000 dims** (e.g. 768, 1024,
1536) works out of the box — the repository zero-pads every written and
queried vector to exactly 2000 dims (padding leaves cosine/L2 distances
unchanged). Models above 2000 dims are rejected with a clear error on write
and search; the health checker flags them as `embedding_dimension_mismatch`.

Existing databases whose rows were written at another width by an earlier
build: re-run `./scripts/migrate.sh up` (idempotent) and re-ingest, or
`ALTER TABLE agent_memories ALTER COLUMN embedding TYPE vector(2000)` after
backing up if the column width must be normalized in place.

## Upgrading existing databases

Module migrations are tracked in their own table, `schema_migrations_modules`,
separate from main's `schema_migrations` (core). A database still on the former
single counter (module version in `schema_migrations`) is converted
automatically once at startup: module progress moves to
`schema_migrations_modules` and the core counter is rewound to the baseline so
core migrations added by `main` are applied (see
`internal/database/migration_legacy.go`). No manual `force` is needed.

Every module statement is idempotent (`CREATE TABLE IF NOT EXISTS`,
`ADD COLUMN IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`), so re-running is
safe. Fresh databases need no action.

## Rollback

- The provided `down` files only revert additive/idempotent changes and exist
  for development databases.
- Behavior is rolled back with feature flags (`memory_v2.enabled`,
  `memory_v2.worker_mutations`), not down-migrations: every module migration
  is additive. `./scripts/migrate.sh force` operates on the core stream only.
## Knowledge-base tenant scope (shared KBs)

Memories belong to the **knowledge base owner's tenant**, not to the tenant of
whoever wrote them, so every member with access to a shared KB sees the same
memory (`user_id` only records who authored a row). Rows written by an earlier
build under the caller's tenant are detected at startup (`agent_memories.tenant_id`
differs from `knowledge_bases.tenant_id`) and moved to the owner tenant; each
move is logged in `agent_memories_scope_repair_log` (migration 900078).
Disable with `MEMORY_V2_KB_SCOPE_REPAIR=off`; use `=audit` to only log findings.

## Background workers

Workers iterate live (tenant, knowledge base) scopes and use each knowledge
base's own models. `memory_v2.worker_mutations` (env
`MEMORY_V2_WORKER_MUTATIONS`, default on) is the kill switch for all workers
that modify memories; while off none starts and `POST /memories/dream` only
returns proposals. Per worker:

- **Dreamer** (default on, env `MEMORY_V2_DREAMER_ENABLED`): one pass per
  changed knowledge base, locked per scope in `dreamer_state` (900081) with a
  heartbeat TTL. Allowed: verdict changes among refuted/gotcha/wip/none and
  importance ±1; never to or from a protected verdict (decision, fixed). Merges
  are proposals for the first `merge_live_after_runs` passes, then require a
  server-side cosine ≥ `merge_min_cosine`. Remove is off. A circuit breaker,
  a per-tenant daily token budget and an `updated_at` re-check guard every
  pass. Changes go through the service, so each one lands in
  `agent_memories_audit` and merges can be undone (`POST /memories/:id/undo-merge`).
  Reviewing a memory sets `metadata.dreamed_at` without bumping `updated_at`.
- **Consolidator** (default on): hub scores, and importance −1 for memories
  unread and unchanged for a year (floor 0, protected excluded).
- **Pruner** (default on): soft-deletes memories past `expires_at`, and tier-3
  memories never read, importance ≤ 0, older than 30 days; never protected
  ones. Soft-deleted rows are purged 30 days later.
- **Entity extractor** (default off).
