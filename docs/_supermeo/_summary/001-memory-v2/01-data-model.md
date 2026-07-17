# 01 — Data Model

> Memory v2 Module | Last Update: 2026-07-09
> 
> **Note**: ERD bên dưới là **TARGET state** sau tất cả migrations (000064 ✅ + 000065 📋 + 000066 📋).
> Schema hiện tại (000064) chưa có `verdict`, `hub_score`, `dreamer_state`, HNSW index.

## 1. Entity Relationship Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│ agent_memories                                                    │
├──────────────────────────────────────────────────────────────────┤
│ id (PK)          VARCHAR(36)    UUID, DEFAULT uuid_generate_v4() │
│ tenant_id        VARCHAR(36)    NOT NULL                         │
│ kb_id            VARCHAR(36)    NOT NULL                         │
│ user_id          VARCHAR(36)    DEFAULT ''                       │
│ session_id       VARCHAR(36)    DEFAULT ''                       │
│ content          TEXT           NOT NULL                         │
│ memory_type      VARCHAR(32)    DEFAULT 'episodic'               │
│ importance       INTEGER        DEFAULT 0                        │
│ tier             SMALLINT       DEFAULT 1                        │
│ embedding        vector(1536)   pgvector                         │
│ fingerprint      VARCHAR(64)    SHA256 first 200 normalized chars│
│ verdict          VARCHAR(16)    DEFAULT 'none'                    │
│ hub_score        REAL           DEFAULT 0   graph centrality      │
│ tags             TEXT[]         DEFAULT '{}'                     │
│ metadata         JSONB          DEFAULT '{}'                     │
│ access_count     BIGINT         DEFAULT 0                        │
│ last_accessed_at TIMESTAMPTZ                                     │
│ created_at       TIMESTAMPTZ    DEFAULT CURRENT_TIMESTAMP        │
│ updated_at       TIMESTAMPTZ    DEFAULT CURRENT_TIMESTAMP        │
│ deleted_at       TIMESTAMPTZ    soft delete                      │
│ expires_at       TIMESTAMPTZ    tier-based TTL                   │
└──────────────────────────────────────────────────────────────────┘
         │                           │
         │ FK                         │ FK
         ▼                           ▼
┌─────────────────────────┐  ┌──────────────────────────────────────┐
│ memory_relations         │  │ extraction_queue                      │
├─────────────────────────┤  ├──────────────────────────────────────┤
│ id (PK)    VARCHAR(36)  │  │ id (PK)       VARCHAR(36)            │
│ tenant_id  VARCHAR(36)  │  │ tenant_id     VARCHAR(36)  NOT NULL  │
│ from_uuid  VARCHAR(36)  │  │ memory_uuid   VARCHAR(36)  NOT NULL  │
│ to_uuid    VARCHAR(36)  │  │ status        VARCHAR(16)  'pending' │
│ rel_type   VARCHAR(64)  │  │ attempts      SMALLINT     DEFAULT 0 │
│ weight     REAL         │  │ max_attempts  SMALLINT     DEFAULT 3 │
│ metadata   JSONB        │  │ error_message TEXT                    │
│ created_at TIMESTAMPTZ  │  │ created_at    TIMESTAMPTZ             │
│ deleted_at TIMESTAMPTZ  │  │ updated_at    TIMESTAMPTZ             │
└─────────────────────────┘  └──────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│ dreamer_state                                         │
├──────────────────────────────────────────────────────┤
│ id (PK)       VARCHAR(36)   UUID                     │
│ tenant_id     VARCHAR(36)   NOT NULL, UNIQUE          │
│ last_run_at   TIMESTAMPTZ                             │
│ locked_by     VARCHAR(64)   lock owner (worker ID)    │
│ locked_until  TIMESTAMPTZ   lock expiry               │
│ stats         JSONB         DEFAULT '{}'              │
│ created_at    TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP │
│ updated_at    TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP │
└──────────────────────────────────────────────────────┘
```

## 2. Memory Type (`memory_type`)

| Value | Mô tả | Ví dụ |
|-------|-------|-------|
| `episodic` | Sự kiện, hội thoại đã xảy ra | "Hôm qua tôi đã deploy service X lên production" |
| `semantic` | Kiến thức, khái niệm tổng quát | "Kubernetes là container orchestration platform" |
| `procedural` | Quy trình, cách làm | "Để deploy: build → test → push → kubectl apply" |
| `decision` | Quyết định đã đưa ra | "Chọn PostgreSQL thay vì MongoDB vì ACID" |
| `preference` | Sở thích, cấu hình cá nhân | "Thích dùng VS Code với theme Dark+" |
| `fact` | Sự thật khách quan | "Server chạy trên AWS region ap-southeast-1" |

## 2.5. Verdict Types (`verdict`)

REM-inspired verdict system phân loại trạng thái của từng memory:

| Value | Mô tả | Behavior |
|-------|-------|----------|
| `none` | Chưa được phân loại | Default, hiển thị bình thường |
| `fixed` | Bug/issue đã được fix | Hiển thị, boost relevance |
| `refuted` | Đã bị bác bỏ (thông tin sai) | **Excluded** khỏi search result mặc định |
| `decision` | Quyết định đã được thông qua | Luôn hiển thị, ưu tiên cao |
| `gotcha` | Non-obvious trap/pitfall | Hiển thị kèm warning |
| `wip` | Work in progress | Hiển thị với trạng thái "đang phát triển" |

**Protected verdicts**: `decision`, `fixed` không thể bị LLM tự động downgrade. Chỉ user/admin mới có thể thay đổi.

**Verdict transitions**:
```
none → fixed | refuted | decision | gotcha | wip
wip  → fixed | refuted | decision | gotcha | none
fixed → refuted (nếu tái xuất hiện)
```

## 3. Memory Tier (`tier`)

| Tier | Name | TTL | Access Count Threshold | Mô tả |
|------|------|-----|----------------------|-------|
| 0 | Critical | ∞ (never) | N/A | Critical memories, permanent |
| 1 | Core | 90 days | >5 | Frequently used core memories |
| 2 | Standard | 30 days | 2-5 | Regular memories |
| 3 | Edge | 7 days | <2 | Low-value memories, first to evict |

### Tier Promotion/Demotion Rules

```
Promotion:
  access_count > 10 && tier > 0 → tier--
  importance >= 5 && tier > 0   → tier--
  tagged "critical"              → tier = 0

Demotion:
  access_count < 2 && tier < 3   → tier++ (after 30d idle)
  expired && tier > 0            → tier++ or soft-delete
```

## 4. Importance Score (-5 to +6)

| Score | Level | Criteria |
|-------|-------|----------|
| +5 to +6 | Critical | Contains credentials, deployment instructions, decisions |
| +3 to +4 | High | Contains technical details, configurations, preferences |
| +1 to +2 | Medium | Contains useful context, domain knowledge |
| 0 | Neutral | Default for episodic memories |
| -1 to -2 | Low | Trivial chat, greetings, small talk |
| -3 to -5 | Noise | Empty, redundant, duplicate content |

### Auto-Detection Keywords

**Positive indicators (+1 each)**:
- English: `deploy`, `config`, `error`, `fix`, `bug`, `password`, `token`, `secret`, `production`, `critical`, `important`, `remember`, `note`, `decision`, `prefer`
- Vietnamese: `triển khai`, `cấu hình`, `lỗi`, `sửa`, `quan trọng`, `ghi nhớ`, `quyết định`, `mật khẩu`, `production`

**Negative indicators (-1 each)**:
- English: `hello`, `hi`, `bye`, `thanks`, `ok`, `yes`, `no`, `weather`, `how are you`
- Vietnamese: `xin chào`, `tạm biệt`, `cảm ơn`, `ừ`, `không`, `thời tiết`

## 4.5. Hub Score (Graph Centrality)

`hub_score` là pre-computed graph centrality metric, được tính bởi Consolidation Scheduler mỗi 6h:

```
hub_score = log(1 + degree) × avg_edge_weight

degree     = số lượng quan hệ (in + out) của memory
edge_weight = trung bình weight của các cạnh liên quan
```

| Hub Score | Interpretation | Boost |
|-----------|---------------|-------|
| 0 | No relations (isolated node) | No graph boost |
| 0.1 - 0.5 | Few weak relations | Small boost |
| 0.5 - 1.0 | Moderate connectivity | Medium boost |
| 1.0 - 2.0 | Strongly connected | High boost |
| >2.0 | Hub node (central) | Maximum graph weight |

**Công dụng**: Pre-computed để tránh recursive CTE trong hot search path. Graph score trong hybrid search lookup trực tiếp từ `hub_score`.

## 5. Relation Types (`relation_type`)

| Type | Mô tả | Ví dụ |
|------|-------|-------|
| `supports` | Memory A hỗ trợ/làm rõ memory B | "Error X happens" → supports → "Fix for error X" |
| `contradicts` | Memory A mâu thuẫn memory B | "Use port 8080" → contradicts → "Use port 9090" |
| `follows` | Memory B xảy ra sau memory A | "Started project" → follows → "Deployed project" |
| `justifies` | Decision A giải thích bởi fact B | "Chose PG" → justifies → "PG supports JSONB" |
| `co_tagged` | Cùng chia sẻ ≥2 important tags | Auto-generated |
| `related_to` | Cosine similarity >0.65 | Auto-generated |

## 6. Fingerprint (Structural Dedup)

```
fingerprint = SHA256(normalize(first_200_chars(content)))

normalize:
  1. lowercase
  2. strip punctuation
  3. collapse whitespace
  4. trim
```

Mục đích: phát hiện duplicate chính xác trước khi embedding.

## 7. Embedding Vector

- **Dimension**: 1536 (configurable via `embedding_dimensions`)
- **Model**: Text embedding model (reuse `embedding.Embedder` interface)
- **Index**: HNSW (Hierarchical Navigable Small World) with `m=16, ef_construction=200`
- **Search**: `ef_search=100` for recall-quality balance
- **Metric**: cosine similarity

### Why HNSW over ivfflat

| Aspect | ivfflat | HNSW |
|--------|---------|------|
| Build time | Fast | Slower (one-time) |
| Query speed | O(sqrt(N)) | O(log N) |
| Recall@10 | ~90% | ~98% |
| Memory | Lower | Higher (~10-20% more) |
| Insert speed | Fast | Moderate |

HNSW chosen because:
- 1M+ memories → query speed critical (O(log N) vs O(sqrt(N)))
- Recall improvement 90% → 98% is significant for RAG quality
- Build time is one-time cost during migration
- Memory overhead (10-20%) is acceptable for production

## 8. Soft Delete Pattern

```
deleted_at IS NULL       → active
deleted_at IS NOT NULL   → soft-deleted
```

- Soft-deleted memories excluded from search by default
- `IncludeDeleted: true` trong MemoryFilter để include
- Tier-3 soft-deleted → hard-deleted sau 14d bởi Pruner worker

## 9. Search Index Strategy

| Index | Type | Columns | Purpose |
|-------|------|---------|---------|
| `idx_agent_memories_tenant_kb` | B-tree | (tenant_id, kb_id) | Primary access path |
| `idx_agent_memories_tenant_kb_type` | B-tree | (tenant_id, kb_id, memory_type) | Filter by type |
| `idx_agent_memories_verdict` | B-tree | (tenant_id, verdict) | Verdict-based filtering |
| `idx_agent_memories_session` | B-tree | (tenant_id, session_id) | Session-scoped retrieval |
| `idx_agent_memories_expires_at` | B-tree | (tenant_id, kb_id, expires_at) WHERE deleted_at IS NULL | Pruner scan |
| `idx_agent_memories_deleted_at` | B-tree | (deleted_at) WHERE deleted_at IS NOT NULL | Cleanup scan |
| `idx_agent_memories_fingerprint` | UNIQUE | (fingerprint) WHERE deleted_at IS NULL | Dedup |
| `idx_agent_memories_tags` | GIN | (tags) | Tag search |
| `idx_agent_memories_fts` | BM25 | (content, memory_type, importance, tier, tags) | Full-text search |
| `idx_agent_memories_embedding` | HNSW | (embedding vector_cosine_ops) m=16, ef_construction=200 | Vector search |

### HNSW Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `m` | 16 | Good balance: higher = better recall but slower build |
| `ef_construction` | 200 | Build quality: higher = better graph quality |
| `ef_search` | 100 | Query-time: higher = better recall, configurable per query |

## 10. Migration

| Migration | Status | Purpose | Key Changes |
|-----------|--------|---------|-------------|
| **000064** | ✅ Committed | Core schema | CREATE TABLE agent_memories + memory_relations + extraction_queue + indexes |
| **000065** | 📋 Planned | Verdict system | ALTER TABLE agent_memories ADD COLUMN verdict VARCHAR(16) DEFAULT 'none' + ADD COLUMN hub_score REAL DEFAULT 0 + CREATE TABLE dreamer_state + verdict + session indexes |
| **000066** | 📋 Planned | HNSW upgrade | DROP INDEX idx_agent_memories_embedding (ivfflat) → CREATE INDEX with HNSW (m=16, ef_construction=200) |

**Current state**: Migration 000064 đã được commit vào codebase với schema cơ bản (không có verdict, hub_score, dreamer_state). 000065 và 000066 là migrations mới cần tạo.

**Common patterns**:
- **Up**: All DDL use `IF NOT EXISTS` / `IF EXISTS` throughout
- **Down**: `.down.sql` tested and ready for rollback
- **Rollback**: Run `.down.sql` in reverse order (000066 → 000065 → 000064)
- **Guards**: `IF NOT EXISTS` / `IF EXISTS` on all operations
