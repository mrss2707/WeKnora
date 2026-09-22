# Module isolation conventions (`develop` branch)

This branch (`develop`) ships features ahead of `main` (Memory V2, model
preferences, Vietnamese i18n) and periodically merges `main` back in. The
patterns below exist so that merge stays a "few lines of conflict" event
instead of a rewrite. They are extracted from two modules that already prove
the pattern works end-to-end: **Memory V2** (`internal/application/service/
memory_v2/`, `migrations/modules/memory_v2/`) and **model preferences**
(`migrations/modules/model_preferences/`).

Follow this document when adding any new feature module on `develop`. It is
intentionally **not** merged into `PROJECT.md` or `CLAUDE.md` so that this
file itself never conflicts with `main`.

---

## 1. Database migrations

Every module's SQL lives under its own directory, never in `migrations/
versioned/` (that stream belongs to `main`'s core migrations, currently up to
`000084`):

```
migrations/modules/<module_name>/
├── manifest.json
├── README.md
└── postgres/
    ├── 9NNNNN_<module_name>.up.sql
    └── 9NNNNN_<module_name>.down.sql
```

- **Version range**: `900000`–`909999`, reserved exclusively for modules.
  This is enforced in code, not just convention — `internal/database/
  migration_source.go`'s `assembleSource()` rejects (fail-closed,
  `ErrInvalidMigrationSet`) any core file that strays into this range and any
  module file that falls outside it. Core migrations from `main` can advance
  freely without ever colliding with module versions.
- **`manifest.json`** — copy the schema from `migrations/modules/memory_v2/
  manifest.json` or `migrations/modules/model_preferences/manifest.json`:
  `module`, `owner`, `backend`, `reserved_range`, `migrations` (filenames),
  `prerequisites`, `rollback_policy`, `upgrade_guide`.
- **`README.md`** — same structure as the two existing ones: a version|file|
  purpose table, backend scoping note (state explicitly if SQLite/Lite mode
  is unaffected — it reads only `migrations/sqlite/`, never `modules/*/
  postgres/`), prerequisites, and rollback guidance.
- Every statement must be idempotent (`CREATE TABLE IF NOT EXISTS`, `ADD
  COLUMN IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`) so re-running `up` on
  an already-migrated database is always safe.
- **Before merging**: run `./scripts/migrate.sh validate postgres` and, if
  the module could ever touch SQLite, `./scripts/migrate.sh validate
  sqlite`. This runs `cmd/migrate_validate` (pure Go, no DB connection
  needed) and catches duplicate versions, missing up/down pairs, and
  out-of-range files before they ever reach a real database.

A ready-to-copy skeleton lives at `migrations/modules/TEMPLATE/`.

## 2. DI container wiring (`internal/container/`)

One function, one file, one call site:

- Write `register<ModuleName>(container *dig.Container) error` in its own
  file `internal/container/<module_name>.go`. Everything the module needs —
  repository, service, plugins, handler, cleanup hooks — is wired inside
  this function.
- `container.go` gets exactly **one line added**: `must(register
  <ModuleName>(container))`. Never add the module's individual `container.
  Provide(...)` calls directly into `container.go` — that's what causes
  scattered, conflict-prone diffs against `main`'s own additions to that
  file.
- Reference implementation: `internal/container/memory_v2.go` — repository,
  lazily-resolving service, chat pipeline plugin registration, shutdown hook
  via `ResourceCleaner`, and the HTTP handler, all in one function.

**Config-gated modules that override/replace something `main` will also
ship** (e.g. Memory V2 sits next to `main`'s legacy cross-session memory):
follow the fail-closed pattern in `internal/container/cross_session_memory.
go`. If the config flag is off, the function is a no-op. If the flag is on
but the real wiring hasn't landed yet (because it arrives with a future
`main` merge), return a clear error instead of silently booting without the
requested runtime. This turns a silent capability gap into a loud startup
failure — much easier to debug than "why isn't X working."

Add a DI test alongside the new file (see `internal/container/
memory_v2_di_test.go`): resolve every type the module registers, assert
double-registration fails (catches accidental duplicate wiring after a
merge), and assert a dropped provider surfaces a named error.

## 3. HTTP routes (`internal/router/`)

Same "one function, one file, one call site" shape as DI:

- Write `Register<ModuleName>Routes(v1 *gin.RouterGroup, handler *handler.
  <X>Handler, g *rbacGuards)` in its own file `internal/router/
  <module_name>.go`.
- The function **must be nil-safe**: if `handler` is `nil` (module
  disabled), return immediately with no routes registered. Callers should
  never need their own nil guard.
- `router.go` gets exactly two additions: one field on `RouterParams`, one
  call inside `NewRouter`. Reference: `internal/router/memory_v2.go` +
  the two-line diff it required in `router.go`.
- Document role floors (Viewer/Contributor/Admin) in the function's doc
  comment, and write a route test modeled on `internal/router/
  memory_v2_test.go`, which checks three things: (a) every role floor is
  enforced (below rejected, at-or-above allowed), (b) every declared route
  is registered exactly once, (c) a nil handler registers zero routes.

## 4. Domain types & interfaces (`internal/types/`)

- New domain types go in their own file: `internal/types/<module_name>.go`
  (see `internal/types/memory_v2.go`) plus, if the module needs a DI-facing
  interface boundary, `internal/types/interfaces/<module_name>.go` (see
  `internal/types/interfaces/memory_v2.go`).
- Adding a **new field** to an existing shared struct is low-risk and fine
  (e.g. `Model.SortOrder`, `PipelineRequest.EnableMemory`).
- **Do not** insert new elements into the middle of an existing, order-
  sensitive literal that `main` might also modify — e.g. `types.
  Pipeline["chat_history_stream"]` in `internal/types/chat_manage.go`. A
  hand-edited splice into a shared slice literal is exactly the kind of
  hunk that turns into an unresolvable merge conflict when both branches
  touch the same lines. If a module needs to add pipeline stages, prefer a
  small `append`/builder helper the module owns, rather than hand-editing
  the shared slice literal in place.

## 5. i18n

- Adding *content* to an existing locale (new keys under a module's own
  namespace, e.g. `memory.*`) only touches `frontend/src/i18n/locales/
  <locale>.ts` for each of the 5 locale files — no wiring changes needed.
- Adding a **new locale** (not usually needed by a feature module) touches
  four places: `frontend/src/i18n/index.ts`, `frontend/src/i18n/
  resolveDefaultLocale.ts`, `frontend/src/i18n/embed.ts`, `frontend/src/
  i18n/localeKeyAudit.ts`. This is existing repo convention, not something a
  module author needs to change.
- **Do not** touch the default/fallback locale value in core code
  (`types.DefaultLanguage()` in `internal/types/context_helpers.go`) as a
  side effect of adding a module — that's a deployment-wide decision, kept
  in exactly one place on purpose (see §7).

## 6. Frontend feature module (`frontend/src/`)

Mirror the Memory V2 shape exactly — it is fully self-contained, only adds
new files, and never edits shared logic:

```
frontend/src/stores/<module>.ts         (Pinia store — file-based, no
                                          central registration needed;
                                          app.use(pinia) in main.ts is
                                          generic bootstrap, not per-store)
frontend/src/api/<module>/index.ts      (API client)
frontend/src/views/<module>/*.vue       (feature views/components)
```

**Integrating into an existing screen** (e.g. adding a tab to `frontend/
src/views/knowledge/KnowledgeBase.vue`): this repo has no tab-registry or
slot abstraction — every top-level KB tab (`documents`, `wiki`, `graph`,
`memory`) is wired by hand in the same handful of spots in that one file.
This is pre-existing convention (the `wiki`/`graph` tabs do the same thing),
not something to "fix" per-module. When adding a new tab, touch exactly
these points and nothing else in that file:

1. Imports (store + view components) near the top of `<script setup>`.
2. The `subTabs`/tab-list computed, if the new tab has sub-navigation.
3. The `validTabs` const array.
4. The breadcrumb `<template>` block for the wiki-enabled branch.
5. The breadcrumb `<template>` block for the non-wiki branch.
6. The main content `<template>` block that switches on `activeKbTab`.

Keep every other file the module touches new-only. If a future module needs
a standalone top-level page (not a KB tab), add one entry to the central
`routes: [...]` array in `frontend/src/router/index.ts` — that's the
existing, single convention for top-level routes in this codebase.

## 7. Reuse existing cross-cutting abstractions — don't re-hardcode

Two abstractions already exist specifically so modules don't have to
hardcode locale/language-specific literals into shared logic. Route through
them instead of adding a new switch-case or literal:

- **Locale/Accept-Language**: `types.DefaultLanguage()` and `types.
  AcceptLanguageHeader(ctx)` (`internal/types/context_helpers.go`) are the
  single source of truth for the deployment's default locale and for
  building `Accept-Language` header values from request context. See
  `internal/infrastructure/web_fetch/fetcher.go`'s `setBrowserHeaders` for
  the reference call site. Never hardcode a locale string as a fallback in
  a new file — call `types.DefaultLanguage()`.
- **Language-specific text processing**: `internal/infrastructure/
  langdata` is a registry (`langdata.Get(lang).Stopwords`, `.
  ChapterPatterns`, `.QuestionWords`, `.CharsPerToken`, ...) that chunker,
  tokenizer, and query-expansion code already consume instead of inline
  per-language switch statements. A module that needs to add or adjust
  language-specific behavior should register data in `langdata` (a new
  `lang_<code>.go` file, `init()` + `Register(...)`) rather than adding a
  new hardcoded case to `internal/infrastructure/chunker/*.go`,
  `internal/application/service/chat_pipeline/query_expansion.go`, or the
  retriever tokenizers — those are shared, order-sensitive logic that
  `main` is likely to also touch.

## 8. Checklist before merging a new module into `develop`

- [ ] Migration files under `migrations/modules/<name>/postgres/`, versions
      in `900000-909999`, `manifest.json` + `README.md` present.
- [ ] `./scripts/migrate.sh validate postgres` (and `sqlite` if relevant)
      passes.
- [ ] DI wiring is one function in its own `internal/container/<name>.go`,
      one call added to `container.go`.
- [ ] Routes are one function in its own `internal/router/<name>.go`,
      nil-safe, one field + one call added to `router.go`; role-floor test
      included.
- [ ] No hand-edits to order-sensitive shared literals (pipeline slices,
      etc.) — additive only.
- [ ] No new hardcoded locale/language literals in core logic — routed
      through `types.DefaultLanguage()` / `langdata`.
- [ ] Frontend: new files only under `stores/<module>.ts`, `api/<module>/`,
      `views/<module>/`; screen-integration edits confined to the checklist
      in §6.
- [ ] `gofmt -l` clean on every touched Go file; `go build ./...` and
      relevant `go test ./...` pass; `npx vue-tsc --noEmit` clean if
      frontend files changed.
