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

Alternative: `docker compose -f e2e/memory_v2/docker-compose.memory-e2e.yml up --build`. Add `--profile documents` before `up` to build/run docreader too. Do not run both stacks on the same ports. Compose configuration is validated; the host stack is the path exercised by the verification runs. With Compose, seed model URLs must be reachable from the app container:

```sh
go run ./e2e/memory_v2/seed -model-url http://host.docker.internal:11434/v1
```

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

`MEMV2_E2E_FRONTEND_URL` changes the browser origin; `MEMV2_E2E_SEED` points both test clients at another seed output. Playwright MCP exploratory screenshots in `evidence/` are from the synthetic world, exclude MCP tokens, and supplement—not replace—the committed specs.

## Operational checks

```sh
# Three actual scheduled passes; run alone, not concurrently with UI version tests.
MEMORY_V2_DREAMER_INTERVAL=5s ./e2e/memory_v2/stack.sh restart-app
MEMV2_E2E_SCHEDULED=1 go test -tags memory_e2e -count=1 -v -run TestScheduledDreamerSafety ./e2e/memory_v2/mcpclient/
./e2e/memory_v2/stack.sh restart-app   # restore the default interval

# Fresh migration + disposable clone reconstructed at module 900081, then
# upgrade to 900082 and re-embed three legacy rows with the real model.
./e2e/memory_v2/migration_smoke.sh

./scripts/migrate.sh validate postgres
WEKNORA_TEST_POSTGRES_DSN='host=localhost port=55441 user=postgres password=e2e dbname=weknora_e2e sslmode=disable' \
WEKNORA_TEST_PARADEDB=1 go test -race ./internal/application/repository/memory_v2 ./internal/application/service/memory_v2/...
```

The migration smoke uses only a disposable clone of **this E2E database**, not a developer/production database. It creates/removes names prefixed `memv2_migration_`, uses port 18081 and Redis DB 14, and disables mutating workers in the smoke process. Expected schema versions: core **114**, module **900082**, both clean.

PostgreSQL regression tests include: atomic audit rollback on create/update/delete, protected-verdict write/delete races, sub-millisecond optimistic locks, versioned merge participants, graph/vector restoration on undo, re-embedding legacy/mismatched model provenance, shared daily budget reservations across 50 callers, expired scope-lock takeover and model-down behavior. Repository tests exercise the full-text fallback without a BM25 index; Lite readiness tests assert the module stays disabled.

Metrics: `GET /api/v1/memories/metrics` is **SystemAdmin-only**, not tenant Admin. It exports bounded, content-free Prometheus worker runs/actions/blocks/last-run, estimated dreamer tokens, and recall database/model latency. It intentionally omits tenant IDs, queries, content and credentials. Counters are per-process; daily token enforcement is durable PostgreSQL state and survives restarts/replicas.

## Deliberate limits

- The local 3B chat models are safety/operation smoke targets, not evidence of production reasoning quality. Zero proposed actions is a valid pass; model quality needs its own evaluation.
- Hybrid search is top-k, not misleading client-side pagination. Keyword browsing remains genuinely server-paged.
- Graph overview shows the top 30 nodes, then expands the selected neighbourhood. Dense views use stable type codes with full hover/focus readouts and keyboard activation. The three color families are validated all-pairs for light/dark surfaces; the Browse table provides the non-color alternative.
- Unknown legacy embeddings are excluded from model-scoped cosine search. Admin relink **re-embeds** them; keyword recall remains available meanwhile. Relink is synchronous, versioned and safe to retry after partial progress.
- Auto-linking currently runs on the write path with best-effort failure logging; Admin relink is the recovery path. A persistent asynchronous graph-job queue from the original design is not implemented in this delivery.
- New merge audits snapshot relation IDs for exact graph undo. Historical audits without that snapshot cannot reconstruct an exact historical graph; their text restore remains available.
- Entity extraction stays off by default. Its optional in-memory enrichment buffer is not a durable queue.
- This suite exercises 200 seeded rows and 50 concurrent saves, not a large-KB performance benchmark or a long production soak.
- Compose/docreader execution, a clone of a real developer/production database, and every manual role/use-case independently replayed through Playwright MCP are not claimed by these results.
- The existing frontend dependency lockfile audit reports 14 advisories (1 critical, 8 high, 3 moderate, 2 low), including transitive `shell-quote`. This work adds no dependency versions and does not resolve that separate repository-wide security backlog. Do not read passing Memory tests as a clean dependency-security audit.
