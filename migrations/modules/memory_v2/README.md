# Memory V2 module migrations (PostgreSQL)

Native Go + PostgreSQL/pgvector memory module (per-KB, multi-tenant). Schema:
`agent_memories`, `memory_relations`, `extraction_queue`, `dreamer_state`.

## Layout & numbering

| Version | File | Purpose |
|---------|------|---------|
| 900074 | `memory_v2` | Creates `agent_memories` (hybrid vector + BM25 search, tiers), `memory_relations`, `extraction_queue` |
| 900075 | `memory_v2_verdict` | Verdict system on `agent_memories` + `dreamer_state` |
| 900076 | `memory_v2_hnsw` | Replaces ivfflat with HNSW vector index |

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
this directory (`migrations/sqlite/` keeps its own stream — verify with
`./scripts/migrate.sh validate --sqlite`).

## Runtime default

Memory V2 is the default runtime. The legacy cross-session memory engine from
`main` is registered only when the `cross_session_memory` config flag is
enabled, inside one gated block (`internal/container/cross_session_memory.go`).
Data conversion between the two engines is **out of scope**.

## Upgrading existing databases

- Rows `76`..(module versions) already present in `schema_migrations` are kept
  as-is; 900073–900076 re-run safely on the next `up` (every statement is
  idempotent: `CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`,
  `CREATE INDEX IF NOT EXISTS`).
- A database currently at version ≤ 76 would otherwise SKIP the core
  migrations 000073–000076 coming from `main`. Handle once:
  `./scripts/migrate.sh force 72 && ./scripts/migrate.sh up`
  (all files are idempotent — verified: `main` 000073 uses
  `ADD COLUMN IF NOT EXISTS`).
- Fresh databases need no action.

## Rollback

- The provided `down` files only revert additive/idempotent changes and exist
  for development databases.
- Manual rollback of a live DB: `./scripts/migrate.sh force 76` moves the
  version marker back; no destructive down-migration is required.