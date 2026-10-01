## develop-branch module isolation (MANDATORY)

`main` is owned by another team; `develop` merges it often. Goal: every develop feature adds NEW files and touches shared/core files by at most 1-2 lines. Reference module: Memory V2 (`memory_v2` files below). Never edit core inline when a new file or hook can do it.

**Migrations** — two independent streams, each with its own golang-migrate version table (`internal/database/migration.go`, `migration_legacy.go`):
- Core = main's `migrations/versioned` (postgres) / `migrations/sqlite`, table `schema_migrations`. Never add develop SQL there: it would collide with main's next number.
- Modules = develop-only `migrations/modules/<name>/{postgres,sqlite}/9NNNNN_<name>.{up,down}.sql`, range 900000-909999 (next free: 900078), table `schema_migrations_modules`, plus `manifest.json` + `README.md` (copy `migrations/modules/TEMPLATE/`). Core runs first, then modules, so module SQL may depend on core tables. Because the counters are separate, new core migrations from main are always applied, whatever module version the DB is at. Fail-closed range/pair/duplicate checks: `internal/database/migration_source.go`.
- All SQL idempotent (`IF NOT EXISTS`). Mirror a module's SQLite SQL in `modules/<name>/sqlite/` with the same number. Tests: `expectedSQLiteMigrationVersion` is main's core counter (leave it to main); module progress is `expectedSQLiteModuleMigrationVersion` + `versionedSQLiteColumns` in `internal/database/migration_sqlite_versioned_schema_test.go`.
- Run `./scripts/migrate.sh validate postgres` (and `sqlite`). After deploy verify `docker logs WeKnora-app | grep -i migrat` and `select * from schema_migrations, schema_migrations_modules`.
- `scripts/migrate.sh up/force` operate on the core stream only (`schema_migrations`), exactly like main.
- Legacy databases (single counter at 9000xx) are converted once on startup: modules=old version, core rewound to the baseline in `migration_legacy.go` (109 pg / 29 sqlite). Never change those baselines.

**DI** — one `register<Name>(c *dig.Container) error` in `internal/container/<name>.go`; `container.go` gets one line `must(register<Name>(container))`. Never add scattered `Provide` calls there. Add a DI test (full resolve, duplicate registration fails, missing provider named). Config-gated feature that overlaps something main will ship: no-op when off, return an error when on but not wired (see `cross_session_memory.go`).

**Routes** — one nil-safe `Register<Name>Routes(v1, handler, g)` in `internal/router/<name>.go`; `router.go` gets one `RouterParams` field + one call. Document role floors; test: floors enforced, each route registered once, nil handler registers nothing (see `memory_v2_test.go`).

**Interfaces** — never add methods to a core interface (e.g. `MCPEndpointService`); define a new optional interface in `internal/types/interfaces/<name>.go` and type-assert it (see `MCPEndpointTokenRetriever`).

**Types** — new types in `internal/types/<name>.go` (+ `internal/types/interfaces/<name>.go`). Adding a field to a shared struct is OK. Never splice into order-sensitive shared literals (e.g. `types.Pipeline["chat_history_stream"]`); use an `append`/builder helper the module owns.

**Do not touch `cli/` or `mcp-server/` on develop** (reset to main; Memory V2 is delivered via the built-in HTTP MCP, the web UI's `memoryProtocolInstruction.ts` is the only protocol copy).

**Files main may also add** — use a distinct name (e.g. `client/memory_v2.go`, not `memory.go`) to avoid add/add conflicts.

**Frontend** — new files only: `stores/<m>.ts`, `api/<m>/`, `views/<m>/`. Editing an existing screen: keep to the minimal hand-wired spots (for a KB tab in `KnowledgeBase.vue`: imports, `subTabs`, `validTabs`, 2 breadcrumb blocks, content switch). Top-level page: one entry in `frontend/src/router/index.ts`.

**i18n** — new keys under the module's own namespace in all locale files (zh-CN, en-US, ko-KR, vi-VN, ru-RU, ja-JP). No hardcoded locale: use `types.DefaultLanguage()` / `types.AcceptLanguageHeader(ctx)`; language-specific logic goes into a `langdata` `lang_<code>.go` registration, not switch-cases in chunker/query-expansion/tokenizers. Don't change the default locale as a side effect.

**Before finishing** — checklist: modules/ migrations + manifest + validate pass; 1 line in `container.go`; 1 field + 1 call in `router.go`; no edits to shared literals; no hardcoded locale; `gofmt -l` clean, `go build ./...`, relevant `go test`, `npx vue-tsc --noEmit` if frontend changed. If a core-file edit is unavoidable, keep it minimal and state why.
