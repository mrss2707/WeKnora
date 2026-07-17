# 03 — Ingestion Pipeline

> Memory v2 Module | Last Update: 2026-07-09

## 1. Overview

Ingestion pipeline xử lý một memory mới từ raw content đến khi lưu vào database. Pipeline gồm 8 stages tuần tự, mỗi stage có thể reject memory nếu không đạt điều kiện.

```
Raw Content
  │
  ▼
[1] Validate ──────────► REJECT nếu invalid
  │
  ▼
[2] Structural Dedup ──► REJECT nếu fingerprint match exact
  │
  ▼
[3] Semantic Dedup ────► MERGE nếu cosine >0.93
  │                     ► REJECT nếu cosine >0.97 (exact duplicate)
  │
  ▼
[4] Type Detection ────► Xác định memory_type
  │
  ▼
[5] Tag Suggestion ────► Gợi ý tags
  │
  ▼
[6] Quality Scoring ───► Tính importance (-5 to +6)
  │
  ▼
[7] Tier Assignment ───► Xác định tier (0-3)
  │
  ▼
[8] Embed & Store ─────► Embed → INSERT agent_memories
  │
  ▼
[8.5] Lint on Write ──► 6 lint rules → MemoryLintIssue[]
  │
  ▼
[9] Batch Enqueue ─────► Buffer 10 items → 1 LLM call entity extraction
```

## 2. Stage Chi Tiết

### 2.1 Validate

**File**: `internal/application/service/memory_v2/ingestion.go`

```go
func (s *MemoryServiceV2Impl) validate(content string) error
```

| Rule | Threshold | Action |
|------|-----------|--------|
| Min length | ≥10 characters | Reject if too short |
| Max length | ≤10000 characters | Truncate if too long |
| Min non-whitespace | ≥5 characters | Reject if mostly whitespace |
| Empty/Nil | content != "" | Reject if empty |

**Error returned**: `ErrMemoryValidation` với message cụ thể.

### 2.2 Structural Dedup

**File**: `internal/application/service/memory_v2/dedup.go`

```go
func (s *MemoryServiceV2Impl) computeFingerprint(content string) string
func (s *MemoryServiceV2Impl) checkStructuralDedup(ctx, tenantID, fingerprint) (*AgentMemory, error)
```

**Fingerprint algorithm**:
```
1. Lấy 200 ký tự đầu tiên
2. Lowercase
3. Strip punctuation (chỉ giữ letters, numbers, spaces)
4. Collapse whitespace (multiple spaces → single space)
5. Trim
6. SHA256 hash → hex string (64 chars)
```

**Actions**:
- Fingerprint match → return existing memory (skip insert)
- No match → continue pipeline

### 2.3 Semantic Dedup

**File**: `internal/application/service/memory_v2/dedup.go`

```go
func (s *MemoryServiceV2Impl) checkSemanticDedup(ctx, tenantID, embedding) (*AgentMemory, DupAction, error)
```

| Cosine Threshold | Action | Mô tả |
|-----------------|--------|-------|
| > 0.97 | BLOCK | Exact semantic duplicate, reject |
| > 0.93 | MERGE | Near duplicate, merge with existing (cap 3 merges, 2000 chars) |
| ≤ 0.93 | PASS | New unique memory |

**Merge logic**:
- Append new content to existing (truncate at 2000 chars)
- Update `updated_at`
- Increment `access_count`
- Recalculate importance (average)
- Return merged memory (skip new insert)

### 2.4 Type Detection

**File**: `internal/application/service/memory_v2/types.go`

```go
func (s *MemoryServiceV2Impl) detectType(content string) types.MemoryType
```

**Algorithm**: Keyword counting (không dùng LLM)

| MemoryType | English Keywords | Vietnamese Keywords |
|-----------|-----------------|-------------------|
| `procedural` | "how to", "steps", "guide", "process", "run", "execute", "deploy", "build", "configure" | "cách", "bước", "hướng dẫn", "chạy", "triển khai", "cấu hình", "lệnh" |
| `decision` | "decided", "chose", "decision", "opted", "selected", "picked", "went with" | "quyết định", "chọn", "lựa chọn" |
| `preference` | "prefer", "like", "favorite", "setting", "config", "theme", "style" | "thích", "ưa", "cài đặt", "thiết lập" |
| `fact` | "is", "are", "was", "were", "has", "have", "located", "running on" | "là", "ở", "chạy trên", "đang" |
| `semantic` | "means", "defines", "concept", "refers to", "stands for" | "nghĩa là", "định nghĩa", "khái niệm" |
| `episodic` | (default/fallback) | (default/fallback) |

**Accuracy target**: ≥80% trên test set.

### 2.5 Tag Suggestion

**File**: `internal/application/service/memory_v2/tags.go`

```go
func (s *MemoryServiceV2Impl) suggestTags(ctx context.Context, content string, embedding []float32) []string
```

**Algorithm**:
1. Extract noun phrases từ content (simple regex: capitalized words, technical terms)
2. Vector search tìm similar memories → lấy tags phổ biến
3. Merge + dedup → limit 10 tags
4. Sort by frequency

**Tag format**: lowercase, snake_case, max 50 chars.

### 2.6 Quality Scoring

**File**: `internal/application/service/memory_v2/quality.go`

```go
func (s *MemoryServiceV2Impl) scoreQuality(content string, memType types.MemoryType) int
```

**Scoring rubric** (-5 to +6):

| Factor | Score | Condition |
|--------|-------|-----------|
| **Positive indicators** | | |
| Contains code/command | +1 | Has backticks, `kubectl`, `docker`, `git`, etc. |
| Contains URL | +1 | Has `http://` or `https://` |
| Contains decision | +1 | Has decision keywords (EN/VI) |
| Contains error/fix | +2 | Has error keywords + fix/solution context |
| Specific detail | +1 | Has version numbers, dates, file paths |
| Long content | +1 | >200 chars (substantial information) |
| | **Max +6** | |
| **Negative indicators** | | |
| Very short | -1 | <30 chars |
| Greeting only | -2 | Only "hello", "hi", "xin chào" |
| Single word | -3 | Only one word |
| All punctuation | -2 | No meaningful words |
| Redundant | -1 | Repeated phrases ≥3 times |
| | **Min -5** | |

### 2.7 Tier Assignment

**File**: `internal/application/service/memory_v2/tiers.go`

```go
func (s *MemoryServiceV2Impl) assignTier(importance int, tags []string) types.MemoryTier
```

| Importance | Tags | Tier |
|-----------|------|------|
| +5 to +6 | - | 0 (Critical) |
| +3 to +4 | - | 1 (Core) |
| +1 to +2 | - | 2 (Standard) |
| 0 | - | 2 (Standard) |
| -1 to -2 | - | 3 (Edge) |
| -3 to -5 | - | 3 (Edge) |
| Any | Contains "critical" | 0 |
| Any | Contains "permanent" | 0 |

### 2.8 Embed & Store

```go
func (s *MemoryServiceV2Impl) embedAndStore(ctx context.Context, memory *types.AgentMemory) error
```

1. Gọi `embedder.Embed(ctx, memory.Content)` → `[]float32`
2. Set `memory.Embedding`
3. Set `memory.ExpiresAt = now + tier.TTLHours()`
4. Gọi `repo.Save(ctx, memory)`

### 2.8.5 Lint on Write

**File**: `internal/application/service/memory_v2/lint.go`

```go
func (s *MemoryServiceV2Impl) lintOnWrite(ctx context.Context, memory *types.AgentMemory) []types.MemoryLintIssue
```

Sau khi lưu memory, chạy 6 lint rules để phát hiện vấn đề sớm:

| # | Rule | Severity | Description |
|---|------|----------|-------------|
| 1 | `stale_fact_check` | warning | Content chứa timestamp cũ (>90 ngày) → có thể đã lỗi thời |
| 2 | `contradiction_check` | error | Cosine >0.85 với memory có verdict=refuted → potential contradiction |
| 3 | `near_duplicate_warning` | warning | Cosine >0.90 với memory hiện có → suggest merge |
| 4 | `low_quality_alert` | warning | Importance < -2, content < 50 chars → chất lượng thấp |
| 5 | `orphan_risk` | info | 0 tags, 0 relations → có thể bị orphan |
| 6 | `verdict_conflict` | error | Content contradicts memory đã có verdict=decision → conflict |

**Response format**: Lint issues được trả về trong `SaveMemoryResult.LintIssues`. Không block save — issues là advisory.

### 2.9 Batch Entity Extraction

**File**: `internal/application/service/memory_v2/batch_extractor.go`

```go
func (s *MemoryServiceV2Impl) enqueuePostProcessing(ctx context.Context, memory *types.AgentMemory)
```

Thay vì enqueue từng memory riêng lẻ cho entity extraction, batch mode gom 10 memories vào 1 LLM call:

**Batch buffer**:
```go
type ExtractionBuffer struct {
    mu       sync.Mutex
    items    []*types.AgentMemory
    maxSize  int           // 10 items
    flushAfter time.Duration // 30s
    timer    *time.Timer
}
```

**Batch prompt**:
```
Extract entities from the following {N} memory entries.
For each memory, return entities with: name, type (technology|concept|person|organization),
and confidence (0.0-1.0). Only include entities with confidence >= 0.7.
Output as JSON array keyed by memory_id.

Memories:
{memory_1_content}
---
{memory_2_content}
...

Output format:
{"results": {"memory_id_1": {"entities": [...]}, "memory_id_2": {"entities": [...]}}}
```

**Batch advantages**:
- 10 memories → 1 LLM call thay vì 10 calls (90% cost reduction)
- Context window utilization cao hơn
- Entity consistency tốt hơn (cùng 1 context window)

**Skip conditions** (giữ nguyên):
- `len(content) < 50` chars
- `memory_type == "preference"`
- All previous attempts failed

**Auto-link**: Vẫn trigger per-memory sau khi entity extraction hoàn thành (không batch).

## 3. Error Handling & Rollback

Nếu một stage fail:
- **Validate/Dedup/Type/Tags/Score/Tier**: return error ngay, không lưu gì
- **Embed fail**: return error, không lưu
- **Store fail**: return error, không enqueue
- **Enqueue fail**: log warning, không block (memory đã được lưu)

## 4. Performance Targets

| Stage | Target Latency | Note |
|-------|---------------|------|
| Validate | <1ms | Pure string operations |
| Structural Dedup | <5ms | SHA256 + DB lookup |
| Semantic Dedup | <50ms | Vector search (cached embedding) |
| Type Detection | <1ms | Keyword counting |
| Tag Suggestion | <10ms | DB query for similar tags |
| Quality Scoring | <1ms | Rule-based scoring |
| Tier Assignment | <1ms | Lookup table |
| Embed & Store | <200ms | API call to embedding service |
| Lint on Write | <30ms | 2-3 vector searches for contradiction/stale checks |
| Batch Enqueue | <5ms | Append to buffer (non-blocking) |
| **Total (sync)** | **<330ms** | p99 target |
| **Batch Extraction (async)** | **<2000ms** | 10 memories/batch, 1 LLM call |

## 5. Configuration

```yaml
# config/config.yaml
memory_v2:
  max_memory_content_length: 10000
  embedding_dimensions: 1536
  semantic_dedup:
    exact_threshold: 0.97
    near_threshold: 0.93
    max_merges: 3
    merge_max_chars: 2000
  lint_on_write:
    enabled: true
    stale_threshold_days: 90
    contradiction_threshold: 0.85
    near_duplicate_threshold: 0.90
  batch_extraction:
    enabled: true
    buffer_size: 10
    flush_interval: 30s
    max_retries: 3
    model: "gpt-3.5-turbo"  # cheap model for extraction
```
