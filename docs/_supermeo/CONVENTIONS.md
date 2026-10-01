## develop-branch module isolation (MANDATORY)

`main` is owned by another team; `develop` merges it often. Goal: every develop feature adds NEW files and touches shared/core files by at most 1-2 lines. Reference module: Memory V2 (`memory_v2` files below). Never edit core inline when a new file or hook can do it.

**Migrations** — never in `migrations/versioned/` (main's stream; develop DB is already ≥900076, so lower core numbers are silently skipped).
- Postgres: `migrations/modules/<name>/postgres/9NNNNN_<name>.{up,down}.sql`, range 900000-909999 (next free: 900078), plus `manifest.json` + `README.md` (copy `migrations/modules/TEMPLATE/`). Fail-closed check in `internal/database/migration_source.go`.
- All SQL idempotent (`IF NOT EXISTS`). SQLite (Lite) uses the SAME range and numbers: mirror in `migrations/modules/<name>/sqlite/9NNNNN_*.{up,down}.sql`; NEVER add to `migrations/sqlite/` (main's stream, rejected ≥900000 and collides with main's next number). Bump `expectedSQLiteMigrationVersion` + `versionedSQLiteColumns` in `internal/database/migration_sqlite_versioned_schema_test.go`.
- Run `./scripts/migrate.sh validate postgres` (and `sqlite`). After deploy verify with `docker logs WeKnora-app | grep migrat`.

**DI** — one `register<Name>(c *dig.Container) error` in `internal/container/<name>.go`; `container.go` gets one line `must(register<Name>(container))`. Never add scattered `Provide` calls there. Add a DI test (full resolve, duplicate registration fails, missing provider named). Config-gated feature that overlaps something main will ship: no-op when off, return an error when on but not wired (see `cross_session_memory.go`).

**Routes** — one nil-safe `Register<Name>Routes(v1, handler, g)` in `internal/router/<name>.go`; `router.go` gets one `RouterParams` field + one call. Document role floors; test: floors enforced, each route registered once, nil handler registers nothing (see `memory_v2_test.go`).

**Interfaces** — never add methods to a core interface (e.g. `MCPEndpointService`); define a new optional interface in `internal/types/interfaces/<name>.go` and type-assert it (see `MCPEndpointTokenRetriever`).

**Types** — new types in `internal/types/<name>.go` (+ `internal/types/interfaces/<name>.go`). Adding a field to a shared struct is OK. Never splice into order-sensitive shared literals (e.g. `types.Pipeline["chat_history_stream"]`); use an `append`/builder helper the module owns.

**Files main may also add** — use a distinct name (e.g. `client/memory_v2.go`, not `memory.go`) to avoid add/add conflicts.

**Frontend** — new files only: `stores/<m>.ts`, `api/<m>/`, `views/<m>/`. Editing an existing screen: keep to the minimal hand-wired spots (for a KB tab in `KnowledgeBase.vue`: imports, `subTabs`, `validTabs`, 2 breadcrumb blocks, content switch). Top-level page: one entry in `frontend/src/router/index.ts`.

**i18n** — new keys under the module's own namespace in all locale files (zh-CN, en-US, ko-KR, vi-VN, ru-RU, ja-JP). No hardcoded locale: use `types.DefaultLanguage()` / `types.AcceptLanguageHeader(ctx)`; language-specific logic goes into a `langdata` `lang_<code>.go` registration, not switch-cases in chunker/query-expansion/tokenizers. Don't change the default locale as a side effect.

**Before finishing** — checklist: modules/ migrations + manifest + validate pass; 1 line in `container.go`; 1 field + 1 call in `router.go`; no edits to shared literals; no hardcoded locale; `gofmt -l` clean, `go build ./...`, relevant `go test`, `npx vue-tsc --noEmit` if frontend changed. If a core-file edit is unavoidable, keep it minimal and state why.
