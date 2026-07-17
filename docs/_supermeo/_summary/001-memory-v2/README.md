# Memory v2 Module — Tổng quan & Kiến trúc

> **Version**: 0.1.0 | **Status**: Draft | **Last Update**: 2026-07-09

## 1. Mục tiêu

Xây dựng module memory mới cho AI Agent trong WeKnora, hoạt động trong mỗi Knowledge Base (coi KB như 1 project). Module chạy native Go + PostgreSQL/pgvector, kế thừa các pattern đã proven từ SaiMem (hybrid search, tier system, dedup pipeline, entity extraction) nhưng tối ưu cho multi-tenant production.

**KHÔNG đụng vào MemoryService hiện tại** (team khác đang phát triển).

## 2. Kiến trúc tổng quan

```
┌─────────────────────────────────────────────────┐
│ Chat Pipeline (giữ nguyên MemoryPlugin)          │
│   MEMORY_RETRIEVAL ──► memory_v2.RetrieveMemory │
│   MEMORY_STORAGE   ──► memory_v2.AddEpisode     │
└─────────────────────────────────────────────────┘
                         │
         ┌───────────────┴───────────────────────┐
         │  MemoryServiceV2 (interface)           │
         │  - SaveMemory / AddEpisode             │
         │  - RetrieveMemory / Search             │
         │  - ManageMemory / GraphMemory          │
         │  - ConsolidateDream / AssessHealth     │
         └───────────────┬───────────────────────┘
                         │
         ┌───────────────┴───────────────────────┐
         │  MemoryRepositoryV2 (GORM+PG)         │
         │  - agent_memories (HNSW index)        │
         │  - memory_vectors (pgvector)          │
         │  - memory_relations (graph)           │
         │  - extraction_queue + dreamer_state   │
         └───────────────┬───────────────────────┘
                         │
         ┌───────────────┴───────────────────────┐
         │  Cross-Cutting Concerns                │
         │  - TokenBudgetManager (3-mode)        │
         │  - Cache Layer (embedding + result)   │
         │  - Lint on Write (6 rules)            │
         │  - Verdict System (REM-inspired)      │
         └───────────────┬───────────────────────┘
                         │
         ┌───────────────┴───────────────────────┐
         │  Background Workers                    │
         │  - EntityExtractor (batch mode)        │
         │  - AutoLinkWorker (graph)              │
         │  - ConsolidationScheduler (+hub_score) │
         │  - DreamerWorker (LLM consolidation)   │
         │  - Pruner (tier-based expiry)          │
         │  - HealthChecker (daily 6 checks)      │
         │  - CacheWarmer (optional)              │
         └───────────────────────────────────────┘
```

## 3. Các thành phần chính

| Layer | Package | Mô tả |
|-------|---------|-------|
| **Types** | `internal/types/memory_v2.go` | Domain models: AgentMemory, MemoryRelation, ExtractionJob, MemoryFilter, MemoryVerdict, TokenBudgetConfig, DreamerConfig, etc. |
| **Interfaces** | `internal/types/interfaces/memory_v2.go` | MemoryServiceV2, MemoryRepositoryV2 interfaces |
| **Repository** | `internal/application/repository/memory_v2/` | GORM + pgvector implementation (HNSW) |
| **Service** | `internal/application/service/memory_v2/` | Business logic: ingestion, retrieval, management, lint, token budget, dreamer |
| **Workers** | `internal/application/service/memory_v2/` | Background: entity extraction (batch), auto-link, consolidation, prune, dreamer, health checker, cache warmer |
| **Plugin** | `internal/application/service/chat_pipeline/memory.go` | Chat pipeline integration (MODIFIED) |
| **DI** | `internal/container/container.go` | Dependency injection registration (MODIFIED) |
| **Config** | `config/config.yaml` | Runtime configuration (MODIFIED) |
| **Migration** | `migrations/versioned/00006[4-6]_memory_v2.*.sql` | Database schema (core + verdict + HNSW) |
| **UI Design** | [07-ui-design.md](./07-ui-design.md) | Frontend: Memory as KB tab (Browse/Graph/Health/History) |
| **MCP & Agent** | [08-mcp-integration.md](./08-mcp-integration.md) | Agent tool registration, KB context flow, MCP path |
| **Frontend** | `frontend/src/views/memory/` | Vue 3 + TDesign components (NEW — 8 files) |
| **Frontend** | `frontend/src/views/knowledge/KnowledgeBase.vue` | +1 tab (MODIFIED) |

## 4. Data Flow

### 4.1 Ingestion Flow (khi lưu memory mới)

```
User Message ──► MemoryPlugin.handleStorage()
  ──► MemoryServiceV2.AddEpisode()
    ──► validate (10-10000 chars, min 5 non-whitespace)
    ──► structural dedup (SHA256 fingerprint)
    ──► semantic dedup (cosine similarity)
    ──► type detection (keyword counting)
    ──► tag suggestion
    ──► quality scoring (-5 to +6)
    ──► tier assignment
    ──► embed (via embedding.Embedder)
    ──► store (via MemoryRepositoryV2.Save)
    ──► lint on write (6 lint rules → MemoryLintIssue[])
    ──► enqueue batch entity extraction
    ──► enqueue auto-link
```

### 4.2 Retrieval Flow (khi cần memory context)

```
User Query ──► MemoryPlugin.handleRetrieval()
  ──► MemoryServiceV2.RetrieveMemory()
    ──► Cache check (embedding cache → result cache)
    ──► MemoryRepositoryV2.Search() [hybrid]
      ├── BM25 (ParadeDB pg_search)
      ├── Cosine (pgvector HNSW)
      ├── Graph (pre-computed hub_score)
      └── Importance score
    ──► merge scores (weighted blend)
    ──► 2-tier recency boost (short-term ×1.15, long-term decay)
    ──► verdict-based filtering (exclude REFUTED, boost DECISION ×1.2)
    ──► session-scoped boost (same session ×1.3)
    ──► score threshold (min 0.4)
    ──► token budget manager (full/truncated/summary)
    ──► context packing (structured XML + token budget info)
  ──► chatManage.MemoryContext = formatted context
```

### 4.3 Background Workers

```
EntityExtractor: buffer 10 items → flush 30s → 1 LLM call batch extraction
AutoLinkWorker:  co_tagged / cosine >0.65 / justifies links
Consolidator:    every 6h → compute hub_score, decay old, merge near-duplicates
DreamerWorker:   every 1h (gate-locked) → 4-phase LLM consolidation
Pruner:          daily → soft-delete expired, hard-delete tier-3 after 14d
HealthChecker:   daily → 6 health checks → HealthReport
CacheWarmer:     startup + every 30m → pre-warm embedding cache (optional)
```

## 5. REM Integration

Memory v2 tích hợp 3 tính năng chính từ REM (Reference-Endorsed Memory) pattern của SaiCodePCLI:

### 5.1 Verdict System

Phân loại trạng thái của từng memory để kiểm soát hiển thị và độ ưu tiên:

| Verdict | Behavior | Protected |
|---------|----------|-----------|
| `none` | Default, hiển thị bình thường | No |
| `fixed` | Bug đã fix, boost relevance | **Yes** |
| `refuted` | Thông tin sai, excluded khỏi search | No |
| `decision` | Quyết định đã thông qua, ưu tiên cao | **Yes** |
| `gotcha` | Non-obvious trap, hiển thị kèm warning | No |
| `wip` | Đang phát triển, stale warning sau 7d | No |

### 5.2 Dreamer Worker

LLM consolidation worker chạy định kỳ (mỗi 1h, gate-locked):
- **4-phase prompt**: Identify Redundancies → Detect Contradictions → Adjust Importance → Prune Noise
- **Validation**: Confidence ≥0.70, protected verdicts never touched
- **Budget**: Max 4000 tokens/pass, max 5 actions/pass
- **Dry-run mode**: Preview actions without applying

### 5.3 Health Assessment

HealthChecker chạy daily với 6 checks:
1. Orphan detection (0 tags + 0 relations)
2. Stale fact detection (>180d, low importance)
3. Contradiction scan (cosine >0.85)
4. Duplication scan (fingerprint + cosine)
5. Graph fragmentation (isolated node ratio)
6. Verdict consistency (wip >30d)

## 6. Token Budget Strategy

Memory context tiêu thụ LLM context window. TokenBudgetManager đảm bảo không overflow:

| Component | Token Allocation |
|-----------|-----------------|
| Total context window | 8000 |
| System prompt | -2000 |
| User query | -500 |
| Reserved for response | -1500 |
| **Available for memory** | **4000** |
| Memory v2 budget cap | **2000** (50% of available) |

### Three Operating Modes

| Mode | Condition | Behavior |
|------|-----------|----------|
| **Full** | ≤1500 tokens | Include all results with full content |
| **Truncated** | 1500-2500 tokens | Cap each memory at 300 tokens |
| **Summary** | >2500 tokens | LLM summarize all memories compactly |

### Tiered Retrieval

| Tier | When | Budget |
|------|------|--------|
| T1: Always | Every query | 100% |
| T2: Budget | Remaining >500 tokens | Up to 30% extra |
| T3: On-Demand | Explicit agent request | Separate budget |

## 7. Công nghệ sử dụng

| Công nghệ | Mục đích |
|-----------|----------|
| **PostgreSQL + pgvector** | Vector database cho embedding search (HNSW index, m=16) |
| **ParadeDB pg_search** | Full-text BM25 search |
| **Redis** | Embedding cache (TTL 5m) + result cache (TTL 2m) |
| **GORM** | ORM cho Go ↔ PostgreSQL |
| **google/uuid** | UUID generation |
| **embedding.Embedder** | Text → vector embedding (reuse từ codebase) |
| **chat.Chat** | LLM cho entity extraction + dreamer consolidation |

## 8. System Integration

### 8.1 Plugin Compatibility

Memory v2 dùng **bridging approach**: `RetrieveMemory()` trả về `*types.MemoryContext` (interface hiện tại), với structured XML được embed trong `RelatedEpisodes[0].Summary`. Plugin `chat_pipeline/memory.go` hoạt động nguyên trạng — không cần sửa.

### 8.2 DI Container

```go
// Factory pattern: MEMORY_BACKEND env var quyết định implementation
if os.Getenv("MEMORY_BACKEND") == "v2" {
    return v2Service  // MemoryServiceV2 (PostgreSQL)
}
return neo4jService    // MemoryService (Neo4j)
```

### 8.3 Frontend Impact

| Component | Thay đổi | Mức độ |
|-----------|----------|--------|
| Chat flow | Không | — |
| KB Detail Page (`KnowledgeBase.vue`) | +1 tab "Memory" với 4 sub-tabs (Browse/Graph/Health/History) | ~30 dòng template + imports |
| Memory toggle (`GeneralSettings.vue`) | `isNeo4jAvailable` → `memoryBackendAvailable` | 2-3 dòng |
| Memory components | 8 file Vue mới trong `frontend/src/views/memory/` | New |
| Router | Không thay đổi (dùng route KB detail hiện tại + `?tab=memory`) | — |
| Sidebar | Không thay đổi | — |

### 8.4 Migration Coexistence

```
000064 (đã có) → Core schema: agent_memories, memory_relations, extraction_queue
000065 (mới)   → Verdict system: ADD COLUMN verdict, hub_score + CREATE dreamer_state
000066 (mới)   → HNSW upgrade: DROP ivfflat → CREATE HNSW index
```

Rollback: `MEMORY_BACKEND=neo4j` → restart. Không cần migrate down (columns tồn tại unused).

## 9. Multi-Tenant Safety

Tất cả query đều include `tenant_id` trong WHERE clause. Index composite `(tenant_id, kb_id)` đảm bảo isolation giữa các tenant. Verdict filtering thêm 1 layer: `verdict != 'refuted'` mặc định.

## 10. Out of Scope

- **KHÔNG** sửa `internal/application/service/memory/` (Neo4j implementation)
- **KHÔNG** sửa `internal/application/repository/memory/neo4j/`
- **KHÔNG** thay đổi `interfaces.MemoryService` interface signature
- **KHÔNG** sửa `chat_pipeline/memory.go` plugin code (bridging approach)
- **KHÔNG** migrate dữ liệu cũ từ Neo4j
- **KHÔNG** hỗ trợ Lite mode (SQLite) trong phase 1
- **CÓ** thay đổi `internal/container/container.go` (DI factory)
- **CÓ** thêm `internal/handler/memory_v2.go` (REST endpoints)
- **CÓ** thay đổi `KnowledgeBase.vue` (+1 Memory tab, +4 sub-tabs)
- **CÓ** thêm `frontend/src/views/memory/` (8 component files)
- **CÓ** thay đổi `GeneralSettings.vue` (2-3 dòng: Neo4j → backend check)
- **CÓ** thêm migrations 000065 + 000066

## 11. Risk Assessment

Xem chi tiết: [06-risks-and-mitigations.md](./06-risks-and-mitigations.md)

| Risk | Severity | Mitigation |
|------|----------|------------|
| pgvector dimension mismatch | High | Đọc dimension từ embedder, dùng `alter vector` nếu thay đổi |
| ParadeDB BM25 khác SQLite FTS5 | Medium | Calibrate weights riêng, feature flag `search_backend` |
| LLM extraction cost | Medium | Batch 10 items/LLM call, model rẻ, skip < 50 chars |
| Dreamer LLM cost spike | Medium | Model rẻ, gate chặt (1h min), budget cứng 4000 tokens |
| Dreamer hallucinates consolidations | High | Validation layer, dry-run mode, protected verdicts |
| Token budget overflow | Medium | Hard cap 2000 tokens, 3 fallback modes |
| Verdict downgrade by LLM | Medium | Protected verdicts, pre-write guard |
| Memory leak từ goroutines | Medium | Context cancellation, worker pool, graceful shutdown |
| DB connection pool cạn | Medium | Separate pool cho workers, max 5 conns |
| Race condition save/search | Low | READ COMMITTED isolation |
| Multi-tenant data leak | High | tenant_id trong mọi WHERE, composite indexes |
| Performance 1M+ memories | Medium | HNSW index, LIMIT trước scoring, cache layer |
| Soft delete accumulation | Low | Pruner hard-delete tier-3 sau 14d, VACUUM monthly |

## 12. Verification

```bash
# Build
go build ./...

# Test
go test ./internal/application/repository/memory_v2/... -v
go test ./internal/application/service/memory_v2/... -v

# Migration (3 migrations: 000064, 000065, 000066)
make migrate-up

# Dreamer test (dry-run)
go test ./internal/application/service/memory_v2/dreamer_worker_test.go -v

# Health check
go test ./internal/application/service/memory_v2/health_checker_test.go -v

# Integration
MEMORY_BACKEND=v2 docker compose up -d
curl -X POST /api/v1/knowledge-qa \
  -d '{"query": "test", "enable_memory": true}'
docker compose logs app | grep -i "memory"
```

## 13. Implementation Roadmap

### 13.1 Current State (✅ Exists)

| File | Status | Contents |
|------|--------|----------|
| `internal/types/memory_v2.go` | ✅ Exists | `AgentMemory` (basic), `MemoryFilter` (basic), `MemorySearchResult` (basic), `MemoryRelation`, `ExtractionJob`, `MemoryStats` (basic), `MemoryContextV2`, `HybridSearchWeights`, `MemoryV2Config` (basic) |
| `internal/types/interfaces/memory_v2.go` | ✅ Exists | `MemoryServiceV2` + `MemoryRepositoryV2` interfaces (basic methods) |
| `migrations/versioned/000064_memory_v2.*.sql` | ✅ Exists | Core schema: `agent_memories`, `memory_relations`, `extraction_queue` |
| `config/config.yaml` | ✅ Exists | Basic `memory_v2` config section |

### 13.2 Phase 1 — Foundation (Cần tạo/sửa)

| # | Action | File | Dependencies |
|---|--------|------|-------------|
| P1.1 | **Migration 000065** — ADD COLUMN `verdict`, `hub_score`; CREATE TABLE `dreamer_state`; CREATE INDEX `idx_agent_memories_verdict`, `idx_agent_memories_session` | `migrations/versioned/000065_*` | 000064 |
| P1.2 | **Migration 000066** — DROP ivfflat, CREATE HNSW index (m=16, ef=200) | `migrations/versioned/000066_*` | 000065 |
| P1.3 | **Extend AgentMemory** — add `Verdict`, `HubScore` fields + GORM tags | `internal/types/memory_v2.go` | 000065 |
| P1.4 | **Extend MemoryFilter** — add `SessionID`, `Verdicts`, `MinScore`, `DeepGraph` | `internal/types/memory_v2.go` | — |
| P1.5 | **Extend MemorySearchResult** — add `IsStale`, `StaleDays` | `internal/types/memory_v2.go` | — |
| P1.6 | **Add new types** — `MemoryVerdict`, `SaveMemoryResult`, `MemoryLintIssue`, `MemoryHealthIssue`, `TokenBudgetConfig`, `TokenBudgetInfo`, `DreamerConfig`, `DreamResult`, `DreamAction`, `CacheWarmerConfig`, `LintOnWriteConfig`, `RecencyBoostConfig`, `DedupConfig`, `HealthReport` | `internal/types/memory_v2.go` | — |
| P1.7 | **Extend MemoryV2Config** — add all missing config substructs + defaults | `internal/types/memory_v2.go` | P1.6 |
| P1.8 | **Extend MemoryStats** — add `ByVerdict` field | `internal/types/memory_v2.go` | P1.6 |

### 13.3 Phase 2 — Repository (Cần tạo/sửa)

| # | Action | File | Dependencies |
|---|--------|------|-------------|
| P2.1 | **Implement MemoryRepositoryV2** — all CRUD + search + graph + queue + stats | `internal/application/repository/memory_v2/repository.go` | P1 |
| P2.2 | **HNSW search queries** — `CosineSearch` with HNSW operator | Same file | 000066 |
| P2.3 | **Verdict-aware queries** — `Search` excludes `refuted` by default, `UPDATE` guards protected verdicts | Same file | P1.3 |
| P2.4 | **Dreamer state queries** — `TryLock`, `Unlock` on `dreamer_state` | Same file | 000065 |

### 13.4 Phase 3 — Service Layer (Cần tạo)

| # | Action | File | Dependencies |
|---|--------|------|-------------|
| P3.1 | **Ingestion pipeline** — validate → dedup → type → tags → score → tier → embed → lint → batch enqueue | `internal/application/service/memory_v2/ingestion.go` | P2 |
| P3.2 | **Hybrid search** — BM25 + HNSW Cosine + hub_score + importance → merge → 2-tier recency → verdict filter → session boost → score threshold | `internal/application/service/memory_v2/search.go` | P2 |
| P3.3 | **Token budget manager** — 3-mode (full/truncated/summary) + tiered retrieval | `internal/application/service/memory_v2/token_budget.go` | P3.2 |
| P3.4 | **Context packing** — structured XML with verdict, stale_days, hub_score, token_budget attributes | `internal/application/service/memory_v2/context.go` | P3.2 |
| P3.5 | **Lint on write** — 6 lint rules | `internal/application/service/memory_v2/lint.go` | P3.1 |
| P3.6 | **MemoryContext bridge** — `RetrieveMemory()` maps XML to `MemoryContext.RelatedEpisodes[0].Summary` | `internal/application/service/memory_v2/service.go` | P3.4 |

### 13.5 Phase 4 — Background Workers (Cần tạo)

| # | Action | File | Dependencies |
|---|--------|------|-------------|
| P4.1 | **Entity Extractor** — batch buffer (10 items, 30s flush) → 1 LLM call | `internal/application/service/memory_v2/entity_extractor.go` | P2 |
| P4.2 | **Auto-Link Worker** — co_tagged / cosine >0.65 / justifies | `internal/application/service/memory_v2/auto_link_worker.go` | P2 |
| P4.3 | **Consolidation Scheduler** — 6h: hub_score compute, decay, near-duplicate merge | `internal/application/service/memory_v2/consolidator.go` | P2 |
| P4.4 | **Dreamer Worker** — gate system, 4-phase prompt, action validator | `internal/application/service/memory_v2/dreamer_worker.go` | P2 |
| P4.5 | **HealthChecker** — daily: 6 health checks | `internal/application/service/memory_v2/health_checker.go` | P2 |
| P4.6 | **Pruner** — daily: soft-delete expired, hard-delete tier-3 >14d | `internal/application/service/memory_v2/pruner.go` | P2 |
| P4.7 | **CacheWarmer** — startup + 30m: pre-warm top 100 queries | `internal/application/service/memory_v2/cache_warmer.go` | P2 |

### 13.6 Phase 5 — Integration (Cần sửa)

| # | Action | File | Dependencies |
|---|--------|------|-------------|
| P5.1 | **DI Container** — register both MemoryService implementations with `dig.Name()`, factory based on `MEMORY_BACKEND` | `internal/container/container.go` | P3 |
| P5.2 | **REST Handlers** — 12 endpoints for UI + admin | `internal/handler/memory_v2.go` | P3 |
| P5.3 | **Memory Status API** — `GET /api/v1/tenants/memory-status` | `internal/handler/tenant.go` (modify) | P3 |
| P5.4 | **KB Detail Page** — add `'memory'` to `validTabs`, Memory tab template with 4 sub-tabs | `frontend/src/views/knowledge/KnowledgeBase.vue` | P5.3 |
| P5.5 | **Memory components** — 8 Vue files (Browse, Card, Table, Graph, Health, History, Drawer) | `frontend/src/views/memory/*.vue` | P5.4 |
| P5.6 | **Frontend memory toggle** — `isNeo4jAvailable` → `memoryBackendAvailable` | `frontend/src/views/settings/GeneralSettings.vue` | P5.3 |
| P5.7 | **Config schema** — extend `config/config.yaml` with full MemoryV2Config | `config/config.yaml` | P1.7 |

### 13.7 Phase 6 — Tests (Cần tạo)

| # | Action | File |
|---|--------|------|
| P6.1 | Repository unit tests (CRUD, search, graph, dedup) | `internal/application/repository/memory_v2/repository_test.go` |
| P6.2 | Service unit tests (ingestion, search, token budget, context) | `internal/application/service/memory_v2/service_test.go` |
| P6.3 | Worker unit tests (extractor, dreamer, health checker) | `internal/application/service/memory_v2/worker_test.go` |
| P6.4 | Integration tests (end-to-end: save → search → retrieve) | `internal/application/service/memory_v2/integration_test.go` |
| P6.5 | Handler tests (REST endpoints) | `internal/handler/memory_v2_test.go` |

### 13.8 Dependency Graph

```
P1 (Types) ──► P2 (Repository) ──► P3 (Service) ──► P5 (Integration)
                 └──────────────► P4 (Workers) ──┘
                                                     │
P6 (Tests) ◄─────────────────────────────────────────┘ (parallel with all)
```

### 13.9 Frontend Memory Toggle — Exact Change

```diff
// frontend/src/views/settings/GeneralSettings.vue

- <t-switch :value="isMemoryEnabled" :disabled="!isNeo4jAvailable || memorySaving" @change="handleMemoryChange" />
+ <t-switch :value="isMemoryEnabled" :disabled="!memoryBackendAvailable || memorySaving" @change="handleMemoryChange" />

- <div v-if="!isNeo4jAvailable" class="warning-banner">{{ $t('memoryRequiresNeo4j') }}</div>
+ <div v-if="!memoryBackendAvailable" class="warning-banner">{{ $t('memoryNotAvailable') }}</div>
```

**Backend contract** (`GET /api/v1/tenants/memory-status`):
```json
{
  "backend": "v2",
  "available": true,  
  "neo4j_available": false,
  "memory_count": 1234
}
```

**Store change** (`frontend/src/stores/settings.ts`): Add `memoryBackendAvailable` ref, populate on app init from `/api/v1/tenants/memory-status`.
