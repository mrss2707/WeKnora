# Memory V2 real-model E2E

An isolated multi-user test world for the built-in HTTP MCP and web UI. Successful model calls use real Ollama models; API responses, permissions, search, graph and audit data are not mocked. Failure tests inject an outage through a local forwarding proxy without stopping Ollama.

Verification snapshot: [HTML report](evidence/verification.html), with UI screenshots and an explicit list of release gates still open.

## Quick start

Requirements: Docker, Go matching `go.mod`, Node 22+, Python 3, Ollama with `bge-m3` (1024 dimensions) and `qwen2.5:3b`. No Anthropic gateway or paid API key is required.

```sh
npm ci --prefix frontend
make memory-e2e-up
make memory-e2e-seed
make memory-e2e-test
```

Ports: app `18080`, Vite `15173`, ParadeDB `55441`, Redis `56380`. The Docker containers are named `memv2-e2e-pg` and `memv2-e2e-redis`; `down` removes only those test containers and their ephemeral database. It does not stop other development containers. The host stack does not run docreader because Memory APIs do not need document parsing.

```sh
./e2e/memory_v2/stack.sh status
./e2e/memory_v2/stack.sh restart-app   # rebuild after backend edits
make memory-e2e-down                 # destroys this test world's database
```

Alternative: `docker compose -f e2e/memory_v2/docker-compose.memory-e2e.yml up --build`. Add `--profile documents` before `up` to build/run docreader too. Do not run both stacks on the same ports. Both host and Compose stacks have been exercised: Compose app/docreader build, fresh migrations, real-model seed, 15 MCP tests and all 17 UI tests passed. The loopback outage and scheduled/restart scripts remain host-only checks. With Compose, seed model URLs must be reachable from the app container:

```sh
go run ./e2e/memory_v2/seed -model-url http://host.docker.internal:11434/v1
```

To build/run Compose alongside the host stack, use a separate project, ports, seed and browser states:

```sh
export MEMV2_COMPOSE_PG_PORT=55442 MEMV2_COMPOSE_REDIS_PORT=56381
export MEMV2_COMPOSE_APP_PORT=18082 MEMV2_COMPOSE_WEB_PORT=15174
docker compose -p memv2-release-gate -f e2e/memory_v2/docker-compose.memory-e2e.yml --profile documents up -d --build

go run ./e2e/memory_v2/seed -base http://localhost:18082 \
  -model-url http://host.docker.internal:11434/v1 \
  -out e2e/memory_v2/.run/compose-seed.json
MEMV2_E2E_SEED="$PWD/e2e/memory_v2/.run/compose-seed.json" \
  go test -tags memory_e2e -count=1 -v -skip 'TestModelDown|TestScheduledDreamerSafety' ./e2e/memory_v2/mcpclient/
MEMV2_E2E_SEED="$PWD/e2e/memory_v2/.run/compose-seed.json" \
  MEMV2_E2E_FRONTEND_URL=http://localhost:15174 \
  MEMV2_E2E_STATE_DIR="$PWD/e2e/memory_v2/.run/compose-storage" \
  npm --prefix frontend run test:e2e:memory
python3 -I e2e/memory_v2/docreader_smoke.py
```

Run the MCP and UI suites sequentially. Ports remain loopback-only; the project has separate database/files volumes. Never overwrite the host seed output or storage states with Compose sessions.

The model-outage proxy test requires a **host app**, because it binds a temporary loopback port in the Go test process. Use the host stack for the full suite; do not expose that proxy publicly to make a container reach it.

## Fixture world

| Workspace | Accounts | Knowledge bases / sharing |
|---|---|---|
| Acme | alice Owner, bob Admin, carol Contributor, dave Viewer | acme-eng shared Editor to Partners; acme-hr shared Viewer |
| Beta | erin Owner | beta-ops; Partners member with Editor organization role |
| Gamma | frank Owner | gamma-private; not in Partners; private canary |

Synthetic accounts use `<name>@memv2-e2e.test`, password `Memv2-e2e!`. These are public test credentials, not suitable for any shared or internet-facing deployment. The seed output and Playwright storage states contain actual test bearer tokens: they are ignored by Git and written with mode 0600.

The generator produces **201 inputs** in four KBs, including 50 queries, English/Vietnamese facts, preferences, bug fixes, decisions, contradictions, near-duplicates and a 9,000-character Vietnamese note. One designated near-duplicate is deferred to the dedup test, so the fresh baseline is **200 live rows**: 118 engineering, 44 HR, 28 Beta and 10 Gamma. Two engineering fixtures are explicitly protected through the versioned REST API.

```sh
python3 e2e/memory_v2/fixtures/generate.py
go run ./e2e/memory_v2/seed
```

The seed reuses accounts/models/KBs/organization/endpoints by name. Re-seeding rotates endpoint tokens; existing clients must reload `seed-output.json`. It is not a reset of arbitrary user changes: use `down`, `up`, `seed` for a clean baseline. Tests clean up their generated memories and restore fixture metadata so repeated suites do not accumulate recall distractors.

Model overrides: `MEMV2_E2E_MODEL_URL`, `MEMV2_E2E_EMBED_MODEL`, `MEMV2_E2E_CHAT_MODEL`, or seed flags `-model-url`, `-embed-model`, `-embed-dim`, `-chat-model`. Default chat temperature is zero in dreamer calls. Recall scores depend on the pinned embedding model and dataset.

## MCP client and CLI

```sh
go test -tags memory_e2e -count=1 -v ./e2e/memory_v2/mcpclient/
go run ./e2e/memory_v2/mcpclient/cmd -profile acme-ro
go run ./e2e/memory_v2/mcpclient/cmd -profile acme-rw -tool memory_status
# Arguments are the actual tool's JSON schema, e.g. knowledge_base_id/query/limit:
go run ./e2e/memory_v2/mcpclient/cmd -tool memory_recall -args '{"knowledge_base_id":"<seed KB ID>","query":"JWT key rotation","limit":5}'
```

Profiles: `acme-rw`, `acme-ro`, `acme-eng-only`, `acme-rate`, `acme-disabled`, `beta-rw`, `beta-shared`, `gamma-rw`. Unrestricted endpoints cover **home-workspace** KBs, not shared KBs; `beta-shared` names both Acme KBs explicitly.

A token-free `.mcp.example.json` uses environment variables. To generate a ready local configuration from a seeded profile without committing its secret:

```sh
umask 077
go run ./e2e/memory_v2/mcpclient/cmd -profile acme-rw -mcp-config > e2e/memory_v2/.run/.mcp.json
chmod 600 e2e/memory_v2/.run/.mcp.json
```

Contended near-merges re-evaluate fresh snapshots up to three times. If the scope remains busy, ingestion preserves the incoming fact as a separate row rather than overwriting someone else; exact concurrent saves still converge through fingerprint uniqueness.

Tests cover tools/list, status, save/recall/detail/graph, exact/near dedup, 49 scoped recall queries (gate >=80% recall@5), Vietnamese/cross-lingual queries, editor/viewer shares, canary isolation, KB scope, read-only tools, raw 401/403 for bad/rotated/disabled tokens, endpoint limiting, 50 concurrent saves and a protocol loop. The optional scheduled test is skipped in an ordinary suite.

## Playwright

```sh
npm --prefix frontend run test:e2e:memory
```

`playwright.memory.config.ts` defines `memory-real`, separate from the existing mock project. Global setup logs in all six users through the real login page and saves private storage states. Member sessions select Acme as their active workspace. No API routes are mocked; the stale-response test delays a **real fetched response** to prove it cannot overwrite a newer query.

Cases include server pagination/keyword search; hybrid ranked top-k search; graph overview/node expansion/detail; create validation and duplicates; content/metadata edits; two-user 409/reload; single delete; bulk importance; stats versus list; Viewer/Contributor/Admin controls; token role floors; shared KB access; Gamma denial; dream preview; real audit history; real near-duplicate merge undo; Vietnamese/English labels. Traces on retry may include test tokens: keep `frontend/test-results/` private and ignored.

`MEMV2_E2E_FRONTEND_URL` changes the browser origin; `MEMV2_E2E_SEED` points both test clients at another seed output. `MEMV2_E2E_STATE_DIR` changes the private storage-state directory consistently for global setup, the default Alice project and per-role contexts; always set it when running a second stack. Playwright MCP exploratory screenshots in `evidence/` are from the synthetic world, exclude MCP tokens, and supplement—not replace—the committed specs.

## Operational checks

```sh
# Three actual scheduled passes; run alone, not concurrently with UI version tests.
MEMORY_V2_DREAMER_INTERVAL=5s ./e2e/memory_v2/stack.sh restart-app
MEMV2_E2E_SCHEDULED=1 go test -tags memory_e2e -count=1 -v -run TestScheduledDreamerSafety ./e2e/memory_v2/mcpclient/
./e2e/memory_v2/stack.sh restart-app   # restore the default interval

# Fresh migration + disposable clone reconstructed at module 900081, then
# upgrade to 900083 and re-embed three legacy rows with the real model.
./e2e/memory_v2/migration_smoke.sh

# Host stack only: pause workers, save, restart, verify durable graph job completion.
# Restores mutating workers and deletes only the uniquely created smoke memory.
./e2e/memory_v2/queue_smoke.sh

# Run operational scripts alone, never alongside UI/MCP version tests.
# Temporarily grants SystemAdmin to the synthetic Alice fixture, then restores it.
python3 -I e2e/memory_v2/metrics_smoke.py

# SIGKILL the isolated app while Dreamer holds a lease; wait for natural 5m expiry.
# Finally restore sentinel tags and the default 6h Dreamer interval.
python3 -I e2e/memory_v2/dreamer_restart_smoke.py

./scripts/migrate.sh validate postgres
WEKNORA_TEST_POSTGRES_DSN='host=localhost port=55441 user=postgres password=e2e dbname=weknora_e2e sslmode=disable' \
WEKNORA_TEST_PARADEDB=1 go test -race ./internal/application/repository/memory_v2 ./internal/application/service/memory_v2/...
```

The migration smoke uses only a disposable clone of **this E2E database**, not a developer/production database. It creates/removes names prefixed `memv2_migration_`, uses port 18081 and Redis DB 14, and disables mutating workers in the smoke process. Expected schema versions: core **114**, module **900083**, both clean.

PostgreSQL regression tests include: atomic audit rollback on create/update/delete, protected-verdict write/delete races, sub-millisecond optimistic locks, versioned merge participants, graph/vector restoration on undo, re-embedding legacy/mismatched model provenance, shared daily budget reservations across 50 callers, expired scope-lock takeover and model-down behavior. Repository tests exercise the full-text fallback without a BM25 index; Lite readiness tests assert the module stays disabled.

Metrics: `GET /api/v1/memories/metrics` is **SystemAdmin-only**, not tenant Admin. It exports bounded, content-free Prometheus worker runs/actions/blocks/last-run, dreamer tokens (provider-reported usage when available), and recall database/model latency. It intentionally omits tenant IDs, queries, content and credentials. Counters are per-process; daily token enforcement is durable PostgreSQL state and survives restarts/replicas.

## Release gates: Dreamer quality and scale/soak

Both gates are opt-in, sequential and write 0600 reports under `.run/` (Git-ignored). Run them alone: they share the local model and the app.

```sh
# Dreamer quality: 100 positive + 100 abstain EN/VI cases, applied+audited actions only.
MEMV2_E2E_QUALITY=1 MEMV2_E2E_QUALITY_MODEL=qwen2.5:3b \
  go test -tags memory_e2e -count=1 -timeout=60m -v -run '^TestDreamerQuality$' ./e2e/memory_v2/quality
# Frozen hold-out (new subjects/wording). Only a passing hold-out sets Qualified=true.
MEMV2_E2E_QUALITY_HOLDOUT=1 ... same command

# Production-size batches (default batch_size 30, 5 opportunities + 25 must-not-change per KB, 4 single-language KBs),
# Dreamer on its per-KB model setting; same thresholds as the per-case gate.
MEMV2_E2E_QUALITY_BATCH=1 MEMV2_E2E_QUALITY_REMOTE=anthropic:4 MEMV2_E2E_QUALITY_PROVIDER=anthropic \
  MEMV2_E2E_QUALITY_MODEL=ds/deepseek-flash MEMV2_E2E_QUALITY_DREAMER_MODEL=ds/deepseek-v4-pro \
  go test -tags memory_e2e -count=1 -timeout=60m -v -run '^TestDreamerBatchQuality$' ./e2e/memory_v2/quality
# Backup/restore rehearsal of the isolated E2E database (dump, restore to a disposable DB, compare state + REST).
./e2e/memory_v2/restore_smoke.sh

# Scale/soak: real bge-m3 vectors, three synthetic tenants, REST reads at 10 req/s,
# MCP model-backed recall/save at 1/10/25/50 sessions, then integrity + resource trend.
MEMV2_E2E_LOAD=1 MEMV2_E2E_LOAD_ROWS=10000 MEMV2_E2E_LOAD_PHASE_SECONDS=120 MEMV2_E2E_LOAD_SOAK_MINUTES=60 \
  go test -tags memory_e2e -count=1 -timeout=6h -v -run '^TestScaleAndSoak$' ./e2e/memory_v2/load
```

Pre-declared pass criteria (constants in the tests; a failure is fixed, never re-scored): quality precision >= 95% and >= 80% of positives correct with zero protected/scope violations (zero actions on a positive case is a failure); non-model reads p95 <= 1s and p99 <= 2s at 10 req/s; model-backed calls p95 <= 15s; unexpected failure rate <= 0.1% (expected 401/403/404/409/429 are counted separately and never reclassified); zero cross-tenant reads; graph queue drains within 10 minutes with no terminal failures; every acknowledged save retrievable and audited; protected rows unchanged; RSS, goroutines, heap and PostgreSQL connections do not trend upward by more than 25% between steady-state windows.

Scale rows are inserted through the production atomic create+audit+graph-queue transaction with real embeddings and distinct fixture facts, deliberately bypassing semantic ingestion dedup (cardinality setup, not an ingestion benchmark). The measured traffic goes through public APIs. Results describe this host and Docker allocation only (record the hardware printed in the report); they are not production capacity guarantees.

## Deliberate limits

- Split configuration (planned production): `MEMV2_E2E_QUALITY_MODEL=ds/deepseek-flash` as the KB summary model plus `MEMV2_E2E_QUALITY_DREAMER_MODEL=ds/deepseek-v4-pro` set per KB through `PUT /memories/settings`: development 86/100 positives, precision 100%, 0 safety violations; every Dreamer call went to v4-pro.
- Hosted candidates are opt-in via `MEMV2_E2E_QUALITY_REMOTE=<id in ~/.paicode/providers.json>` (+ `MEMV2_E2E_QUALITY_MODEL`, optional `MEMV2_E2E_QUALITY_PROVIDER=anthropic`); the key is read at run time, only the synthetic corpus is sent. Latest: `ds/deepseek-v4-pro` passes development and hold-out (85/100, precision 98.8%/96.6%, 0 safety violations).
- The local 3B chat models are safety/operation smoke targets, not evidence of production reasoning quality. Zero proposed actions is a valid pass; model quality needs its own evaluation.
- Hybrid search is top-k, not misleading client-side pagination. Keyword browsing remains genuinely server-paged.
- Graph overview shows the top 30 nodes, then expands the selected neighbourhood. Dense views use stable type codes with full hover/focus readouts and keyboard activation. The three color families are validated all-pairs for light/dark surfaces; the Browse table provides the non-color alternative.
- Unknown legacy embeddings are excluded from model-scoped cosine search. Admin relink **re-embeds** them; keyword recall remains available meanwhile. Relink is synchronous, versioned and safe to retry after partial progress.
- Graph jobs are persisted in `extraction_queue` in the same transaction as memory writes. A background worker retries failed links from stored, verified vectors, uses expiring leases and generation fencing, and stops at ten attempts. Version-checked endpoint locks prevent stale jobs from recreating edges to deleted memories. Foreground linking remains a best-effort low-latency path; the queue guarantees a durable retry opportunity. Admin relink is the explicit recovery path after terminal failures or legacy re-embedding.
- New merge audits snapshot relation IDs for exact graph undo. Historical audits without that snapshot cannot reconstruct an exact historical graph; their text restore remains available.
- Entity extraction stays off by default. Its optional in-memory enrichment buffer is not a durable queue.
- This suite exercises 200 seeded rows and 50 concurrent saves, not a large-KB performance benchmark or a long production soak.
- Docreader has been built and run in the isolated `memv2-release-gate` Compose project: gRPC health plus `ListEngines`, `Read` and `ReadStream` passed with Vietnamese UTF-8 Markdown (`python3 -I e2e/memory_v2/docreader_smoke.py`). Compose app/frontend E2E passed with 15 MCP tests (recall@5 45/49) and 17 UI tests; post-run invariants were 200 live memories, 226 graph jobs done and zero invalid live edges. A clone of a real developer/production database and every manual role/use-case independently replayed through Playwright MCP are still not claimed by these results.
- The existing frontend dependency lockfile audit reports 14 advisories (1 critical, 8 high, 3 moderate, 2 low), including transitive `shell-quote`. This work adds no dependency versions and does not resolve that separate repository-wide security backlog. Do not read passing Memory tests as a clean dependency-security audit.
