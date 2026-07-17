# 04 — Search & Retrieval

> Memory v2 Module | Last Update: 2026-07-09

## 1. Hybrid Search Architecture

Memory v2 sử dụng hybrid search kết hợp 4 scoring signals:

```
┌──────────────────────────────────────────────────────────────┐
│                        User Query                             │
└──────────────────────────┬───────────────────────────────────┘
                           │
          ┌────────────────┼────────────────┐
          ▼                ▼                 ▼
    ┌──────────┐    ┌────────────┐    ┌───────────┐
    │  Embed   │    │ ParadeDB   │    │  Graph    │
    │  (API)   │    │  BM25 FTS  │    │  Score    │
    └────┬─────┘    └─────┬──────┘    └─────┬─────┘
         │                │                 │
         ▼                ▼                 ▼
    Cosine Score     BM25 Score        Graph Score     Importance Score
     (0-1 range)     (0-1 range)       (0-1 range)      (0-1 range)
         │                │                 │                │
         └────────────────┼─────────────────┼────────────────┘
                          │                  │
                          ▼                  ▼
                    ┌─────────────────────────────┐
                    │  Weighted Blend + Normalize  │
                    │  BM25=0.15, Cosine=0.45,    │
                    │  Graph=0.25, Importance=0.15│
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │  2-Tier Recency Boost       │
                    │  Short-term: ×1.15 (<1h)    │
                    │  Long-term: 1+0.05×e^(-d/30)│
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │   Verdict-Based Filtering   │
                    │   Default: exclude REFUTED  │
                    │   Boost: ×1.2 for DECISION  │
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │   Session-Scoped Boost      │
                    │   Same session: ×1.3        │
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │   Score Threshold: Min 0.4  │
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │   Token Budget Manager      │
                    │   Full/Truncated/Summary    │
                    │   + Tiered Retrieval        │
                    └────────────┬────────────────┘
                                 │
                                 ▼
                    ┌─────────────────────────────┐
                    │     Ranked Results           │
                    │     (top-K, sorted)          │
                    └─────────────────────────────┘
```

### Cache Layer

```
┌──────────────────────────────────────────────────────────┐
│ Search Request                                            │
└──────────┬───────────────────────────────────────────────┘
           │
           ▼
     ┌─────────────┐    HIT     ┌──────────────────┐
     │ Embedding   │──────────►│ Skip embed call   │
     │ Cache (5m)  │            │ Reuse embedding   │
     └─────┬───────┘            └──────────────────┘
           │ MISS
           ▼
     ┌─────────────┐
     │ Embed API   │
     └─────┬───────┘
           │
           ▼
     ┌─────────────┐    HIT     ┌──────────────────┐
     │ Result      │──────────►│ Return cached     │
     │ Cache (2m)  │            │ results directly  │
     └─────┬───────┘            └──────────────────┘
           │ MISS
           ▼
     ┌─────────────────────────┐
     │ Full Hybrid Search      │
     │ (BM25 + HNSW + Graph)   │
     └─────────────────────────┘
```

**Cache invalidation**: New memory saved in same tenant → invalidate result cache for that tenant.

## 2. Scoring Signals

### 2.1 BM25 Score (Weight: 0.15)

**Source**: ParadeDB `pg_search` extension

```sql
SELECT id, paradedb.score(id) as bm25_score
FROM agent_memories.search(
    query => paradedb.parse($1),
    limit_rows => $2
)
WHERE tenant_id = $3
  AND deleted_at IS NULL
```

**Normalization**: `score / max(score_in_batch)` → 0-1 range.

### 2.2 Cosine Score (Weight: 0.45)

**Source**: pgvector `vector_cosine_ops`

```sql
SELECT id, 1 - (embedding <=> $1) as cosine_score
FROM agent_memories
WHERE tenant_id = $2
  AND deleted_at IS NULL
ORDER BY embedding <=> $1
LIMIT $3
```

**Note**: `<=>` is cosine distance (0-2). `1 - distance` = cosine similarity (range -1 to 1). Scores <0 clamped to 0.

### 2.3 Graph Score (Weight: 0.25)

**Source**: Pre-computed `hub_score` field on `agent_memories` (recalculated every 6h by Consolidation Scheduler)

```sql
SELECT id, hub_score / MAX(hub_score) OVER () as graph_score
FROM agent_memories
WHERE tenant_id = $1
  AND deleted_at IS NULL
  AND id = ANY($2)
```

**Fallback (deep graph traversal)**: Nếu cần graph depth >1 (T3 retrieval), dùng recursive CTE:

```sql
WITH RECURSIVE graph_traversal AS (
    -- Base: direct relations
    SELECT to_uuid as related_id, weight
    FROM memory_relations
    WHERE from_uuid = $1 AND deleted_at IS NULL
    
    UNION
    
    -- Recursive: depth-2
    SELECT mr.to_uuid, mr.weight * gt.weight * 0.5
    FROM memory_relations mr
    JOIN graph_traversal gt ON mr.from_uuid = gt.related_id
    WHERE mr.deleted_at IS NULL
)
SELECT related_id, SUM(weight) as graph_score
FROM graph_traversal
GROUP BY related_id
```

**Hub centrality**: Pre-computed daily; hot search path lookups `hub_score` directly (no CTE).

**Adaptive**: Nếu graph empty (không có relations) → redistribute weight:
- BM25 +0.10 (0.25)
- Importance +0.15 (0.30)
- Graph = 0
- Cosine giữ nguyên 0.45

### 2.4 Importance Score (Weight: 0.15)

```go
importanceScore = float64(memory.Importance + 5) / 11.0
// Maps -5..+6 → 0..1
```

## 3. Score Merge & Normalization

```go
func mergeScores(results []*MemorySearchResult, weights HybridSearchWeights) {
    // 1. Normalize từng component score về 0-1 range
    // 2. Weighted sum
    for _, r := range results {
        r.FinalScore = 
            r.BM25Score * weights.BM25 +
            r.CosineScore * weights.Cosine +
            r.GraphScore * weights.Graph +
            r.ImportanceScore * weights.Importance
    }
    // 3. Recency boost (2-tier)
    for _, r := range results {
        ageHours := time.Since(r.Memory.CreatedAt).Hours()
        if ageHours < 1 {
            r.FinalScore *= 1.15 // Short-term boost
        } else {
            ageDays := ageHours / 24.0
            r.FinalScore *= 1 + 0.05 * math.Exp(-ageDays/30.0) // Long-term decay
        }
    }
}
```

## 4. Recency Boost (2-Tier)

### Short-Term Boost (<1 hour)

```
boost = 1.15  (flat, for all memories created <1h ago)
```

Purpose: Recent conversations are likely still relevant — aggressive boost for very fresh memories.

### Long-Term Boost (≥1 hour)

```
boost = 1 + 0.05 × e^(-age_hours / (30 × 24))

age_hours = 1    → boost = 1.050  (just crossed threshold)
age_hours = 24   → boost = 1.048  (1 day)
age_days  = 7    → boost = 1.040  (1 week)
age_days  = 30   → boost = 1.018  (1 month)
age_days  = 90   → boost = 1.002  (3 months)
age_days  = 365  → boost = 1.000  (negligible)
```

## 5. Token Budget Manager

Memory context consumes LLM context window tokens. TokenBudgetManager ensures memory doesn't overflow the budget.

### 5.1 Three Modes

| Mode | Condition | Behavior |
|------|-----------|----------|
| **Full** | Total tokens ≤ `truncate_threshold` (1500) | Include all results with full content |
| **Truncated** | Tokens between 1500-2500 | Truncate each memory to `max_per_memory` (300 tokens) |
| **Summary** | Tokens > `summary_threshold` (2500) | LLM summarize all memories into compact format |

### 5.2 Budget Breakdown

```
Total context window:  8000 tokens
System prompt:         -2000 tokens
User query:            -500  tokens
Reserved for response: -1500 tokens
─────────────────────────────────
Available for memory:   4000 tokens
Memory v2 budget cap:   2000 tokens (50% of available)
Actually used (typical): ~450 tokens
```

### 5.3 Implementation

```go
func (tb *TokenBudgetManager) Apply(results []*MemorySearchResult, budget TokenBudgetConfig) ([]*MemorySearchResult, TokenBudgetInfo) {
    totalTokens := tb.estimateTokens(results)
    
    switch {
    case totalTokens <= budget.TruncateThreshold:
        // Full mode: keep all
        return results, TokenBudgetInfo{Mode: "full", Used: totalTokens, Remaining: budget.MaxTotalTokens - totalTokens}
        
    case totalTokens <= budget.SummaryThreshold:
        // Truncated mode: cap per memory
        for _, r := range results {
            r.Memory.Content = tb.truncate(r.Memory.Content, budget.MaxPerMemory)
        }
        return results, TokenBudgetInfo{Mode: "truncated", Used: len(results) * budget.MaxPerMemory}
        
    default:
        // Summary mode: LLM compact
        summary, _ := tb.summarize(ctx, results)
        return summary, TokenBudgetInfo{Mode: "summary", Used: tb.estimateTokens(summary)}
    }
}
```

## 6. Tiered Retrieval

Three tiers of retrieval depth, gated by token budget:

| Tier | Description | When | Budget Allocation |
|------|-------------|------|-------------------|
| **T1: Always** | Top-K hybrid search results | Every query | 100% of budget |
| **T2: Budget** | Graph neighbors (depth-1) | If budget remaining >500 tokens | Up to 30% extra |
| **T3: On-Demand** | Deep graph (depth-2), entity expansion | Explicit agent request only | Separate budget |

```go
func (s *MemoryServiceV2Impl) tieredRetrieval(ctx context.Context, query string, filter *MemoryFilter) ([]*MemorySearchResult, error) {
    // T1: Always execute
    results := s.hybridSearch(ctx, query, filter)
    
    // T2: Only if budget allows
    if s.tokenBudget.Remaining() > 500 {
        neighbors := s.expandGraphNeighbors(ctx, results, depth=1)
        results = append(results, neighbors...)
    }
    
    // T3: On-demand (flag in filter)
    if filter.DeepGraph {
        deepResults := s.expandGraphNeighbors(ctx, results, depth=2)
        results = append(results, deepResults...)
    }
    
    return results, nil
}
```

## 7. Verdict-Based Filtering

```go
func applyVerdictFilter(results []*MemorySearchResult, filter *MemoryFilter) []*MemorySearchResult {
    excludeVerdicts := map[MemoryVerdict]bool{}
    
    // Default: exclude REFUTED (if not explicitly requested)
    if filter.Verdicts == nil {
        excludeVerdicts[VerdictRefuted] = true
    }
    
    filtered := make([]*MemorySearchResult, 0, len(results))
    for _, r := range results {
        if excludeVerdicts[r.Memory.Verdict] {
            continue
        }
        // Boost DECISION verdicts
        if r.Memory.Verdict == VerdictDecision {
            r.FinalScore *= 1.2
        }
        // Boost FIXED verdicts slightly
        if r.Memory.Verdict == VerdictFixed {
            r.FinalScore *= 1.1
        }
        filtered = append(filtered, r)
    }
    return filtered
}
```

**Verdict Filtering Rules**:
- `refuted` → excluded by default (can be included with `Verdicts: ["refuted"]`)
- `decision` → ×1.2 score boost
- `fixed` → ×1.1 score boost
- `gotcha` → included with warning flag
- `wip` → included with stale warning if >7 days

## 8. Session-Scoped Retrieval

```go
func applySessionBoost(results []*MemorySearchResult, sessionID string) {
    if sessionID == "" {
        return
    }
    for _, r := range results {
        if r.Memory.SessionID == sessionID {
            r.FinalScore *= 1.3  // +30% boost for same session
        }
    }
}
```

Purpose: Trong cùng 1 phiên chat, memories từ phiên đó có relevance cao hơn.

## 9. Score Threshold Filtering

```go
const MinScoreThreshold = 0.4

func applyScoreThreshold(results []*MemorySearchResult, minScore float64) []*MemorySearchResult {
    if minScore <= 0 {
        minScore = MinScoreThreshold
    }
    filtered := make([]*MemorySearchResult, 0, len(results))
    for _, r := range results {
        if r.FinalScore >= minScore {
            filtered = append(filtered, r)
        }
    }
    return filtered
}
```

Scores below 0.4 are noise — exclude trước khi packing context.

## 10. Freshness Warnings

```go
func checkFreshness(memory *AgentMemory) (bool, int) {
    if memory.CreatedAt.IsZero() {
        return false, 0
    }
    days := int(time.Since(memory.CreatedAt).Hours() / 24)
    return days > 30, days
}
```

| Stale Days | Warning Level | Behavior |
|------------|---------------|----------|
| 0-30 | None | Normal display |
| 31-90 | Low | `stale_days` attribute in XML, no visual change |
| 91-180 | Medium | `is_stale=true` in result, `⚠` indicator |
| >180 | High | Boost demotion ×0.8, warning in context |

## 11. Context Packing

Kết quả search được format thành structured XML context để inject vào prompt:

```go
func (s *MemoryServiceV2Impl) packContext(
    query string,
    results []*types.MemorySearchResult,
    budget TokenBudgetInfo,
) string {
    var buf strings.Builder
    buf.WriteString("<memory_context>\n")
    buf.WriteString(fmt.Sprintf("  <query>%s</query>\n\n", xmlEscape(query)))
    buf.WriteString("  <results>\n")
    
    for _, r := range results {
        m := r.Memory
        staleDays := int(time.Since(m.CreatedAt).Hours() / 24)
        buf.WriteString(fmt.Sprintf(
            `    <memory id="%s" type="%s" importance="%d" tier="%d" score="%.2f" verdict="%s" stale_days="%d" hub_score="%.1f">`+"\n",
            m.ID, m.MemoryType, m.Importance, m.Tier, r.FinalScore,
            m.Verdict, staleDays, m.HubScore,
        ))
        buf.WriteString(fmt.Sprintf("      %s\n", xmlEscape(m.Content)))
        buf.WriteString("    </memory>\n")
    }
    
    buf.WriteString("  </results>\n")
    buf.WriteString(fmt.Sprintf("  <stats total=\"%d\" by_type=\"%s\" />\n", len(results), typeSummary(results)))
    buf.WriteString(fmt.Sprintf("  <token_budget used=\"%d\" remaining=\"%d\" mode=\"%s\" />\n",
        budget.Used, budget.Remaining, budget.Mode))
    buf.WriteString("</memory_context>")
    
    return buf.String()
}
```

## 12. Search Query Examples

### BM25-only search
```sql
SELECT id, content, paradedb.score(id) as score
FROM agent_memories.search(
    query => paradedb.parse('deploy kubernetes production'),
    limit_rows => 20
)
WHERE tenant_id = 'tenant-1'
ORDER BY score DESC;
```

### Cosine-only search
```sql
SELECT id, content, 1 - (embedding <=> '[0.1, 0.2, ...]'::vector) as score
FROM agent_memories
WHERE tenant_id = 'tenant-1' AND deleted_at IS NULL
ORDER BY embedding <=> '[0.1, 0.2, ...]'::vector
LIMIT 20;
```

### Graph traversal from memory
```sql
WITH RECURSIVE related AS (
    SELECT to_uuid, weight, 1 as depth
    FROM memory_relations
    WHERE from_uuid = 'abc-123' AND deleted_at IS NULL
    
    UNION ALL
    
    SELECT mr.to_uuid, mr.weight * r.weight * 0.5, r.depth + 1
    FROM memory_relations mr
    JOIN related r ON mr.from_uuid = r.to_uuid
    WHERE mr.deleted_at IS NULL AND r.depth < 3
)
SELECT am.*, SUM(r.weight) as graph_boost
FROM related r
JOIN agent_memories am ON am.id = r.to_uuid
WHERE am.deleted_at IS NULL
GROUP BY am.id;
```

## 13. Performance Optimization

| Optimization | Technique |
|-------------|-----------|
| **Pre-filter** | Apply `tenant_id`, `deleted_at IS NULL`, `verdict != 'refuted'` before scoring |
| **LIMIT early** | BM25/Cosine limit to 100 candidates before full scoring |
| **Parallel queries** | Run BM25 + Cosine + Graph in parallel goroutines |
| **HNSW index** | m=16, ef_construction=200, ef_search=100 for O(log N) search |
| **Connection pool** | Separate pool: search queries dùng main pool, workers dùng worker pool |
| **Embedding cache** | LRU cache query embeddings, TTL 5 minutes |
| **Result cache** | Cache search results per (query_hash, tenant_id), TTL 2 minutes |
| **Score threshold** | Drop results with FinalScore < 0.4 before context packing |
| **Graph pre-compute** | Use `hub_score` instead of recursive CTE for graph weight |

## 14. Configuration

```yaml
memory_v2:
  hybrid_search_weights:
    bm25: 0.15
    cosine: 0.45
    graph: 0.25
    importance: 0.15
  search_backend: "paradedb"  # paradedb | fts5 | simple
  max_search_results: 50
  min_score_threshold: 0.4
  hnsw:
    m: 16
    ef_construction: 200
    ef_search: 100
  recency_boost:
    enabled: true
    short_term_multiplier: 1.15
    short_term_window: 1h
    long_term_factor: 0.05
    long_term_half_life_days: 30
  token_budget:
    max_total_tokens: 2000
    mode: "full"
    truncate_threshold: 1500
    summary_threshold: 2500
    max_per_memory: 300
  cache:
    embedding_ttl: 5m
    result_ttl: 2m
  freshness:
    stale_warning_days: 30
    stale_demotion_days: 180
    demotion_multiplier: 0.8
```
