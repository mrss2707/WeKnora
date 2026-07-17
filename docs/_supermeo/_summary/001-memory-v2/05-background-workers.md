# 05 — Background Workers

> Memory v2 Module | Last Update: 2026-07-09

## 1. Overview

Memory v2 module có 6 background workers chạy độc lập:

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                          Background Workers                                    │
├───────────────┬──────────────┬─────────────────┬────────────┬────────────────┤
│ Entity        │ Auto-Link    │ Consolidation   │ Pruner     │ Dreamer       │
│ Extractor     │ Worker       │ Scheduler       │            │ Worker        │
├───────────────┼──────────────┼─────────────────┼────────────┼────────────────┤
│ Poll every 2s │ Trigger on   │ Every 6 hours   │ Daily      │ Every 1h      │
│ Batch size 10 │ new memory   │                 │            │ (gate-locked) │
│ Flush 30s     │ 3 link types │ Decay + Merge   │ Soft/Hard  │ 4-phase LLM   │
│ LLM-powered   │ Rule-based   │ + Hub Score     │ delete     │ consolidation │
├───────────────┴──────────────┴─────────────────┴────────────┴────────────────┤
│ HealthChecker                                    │ CacheWarmer (optional)     │
├──────────────────────────────────────────────────┼────────────────────────────┤
│ Daily at 4:00 AM                                 │ On startup + every 30m     │
│ 6 health checks                                  │ Pre-warm embedding cache   │
│ → HealthIssue[] report                           │ Top 100 frequent queries   │
└──────────────────────────────────────────────────┴────────────────────────────┘
```

## 2. Entity Extractor Worker

**File**: `internal/application/service/memory_v2/entity_extractor.go`

### 2.1 Workflow (Batch Mode)

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ Buffer       │────►│ Flush Timer  │────►│ LLM Extract  │
│ (append mem) │     │ (10 items OR │     │ Batch (1     │
│              │     │  30s timeout)│     │  call)       │
└──────────────┘     └──────────────┘     └──────┬───────┘
       │                                         │
       │ (per-item)                              ▼
       ▼                                   ┌──────────────┐
  ┌──────────┐                             │ Store        │
  │ Auto-link│                             │ Relations    │
  │ (trigger)│                             │ (batch)      │
  └──────────┘                             └──────────────┘
```

### 2.2 Batch Buffer

```go
type ExtractionBuffer struct {
    mu         sync.Mutex
    items      []*types.AgentMemory
    maxSize    int           // 10 items
    flushAfter time.Duration // 30s
    timer      *time.Timer
    callback   func([]*types.AgentMemory) error
}

func (b *ExtractionBuffer) Append(memory *types.AgentMemory) {
    b.mu.Lock()
    defer b.mu.Unlock()
    
    b.items = append(b.items, memory)
    
    if len(b.items) >= b.maxSize {
        b.flush()
        return
    }
    
    // Reset flush timer on first item
    if len(b.items) == 1 {
        b.timer.Reset(b.flushAfter)
    }
}

func (b *ExtractionBuffer) flush() {
    if len(b.items) == 0 {
        return
    }
    batch := b.items
    b.items = nil
    
    go func() {
        if err := b.callback(batch); err != nil {
            // Individual items retried separately on failure
            for _, m := range batch {
                b.enqueueRetry(m)
            }
        }
    }()
}
```

### 2.3 Retry Queue (Fallback)

Khi batch extraction fail, individual items được enqueue riêng vào extraction_queue để retry:

```sql
UPDATE extraction_queue
SET status = 'running', attempts = attempts + 1, updated_at = NOW()
WHERE id = (
    SELECT id FROM extraction_queue
    WHERE status = 'pending'
    ORDER BY created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;
```

`SKIP LOCKED` đảm bảo multiple workers không conflict.

### 2.4 Individual LLM Prompt (Fallback)

```
Extract entities from the following memory content.
Return JSON with entities having: name, type (technology|concept|person|organization),
and confidence (0.0-1.0). Only include entities with confidence >= 0.7.

Memory:
{content}

Output format:
{"entities": [{"name": "...", "type": "...", "confidence": 0.XX}]}
```

### 2.5 Store as Relations

Entities được lưu dưới dạng `memory_relations` rows (KHÔNG tạo entity table riêng):

```go
for _, entity := range extractedEntities {
    // Find memories that mention same entity → create co_tagged relation
    similar, _ := w.repo.BM25Search(ctx, filter, entity.Name)
    for _, similar := range similar {
        if similar.Memory.ID != memory.ID {
            w.repo.SaveRelation(ctx, &types.MemoryRelation{
                TenantID:     memory.TenantID,
                FromUUID:     memory.ID,
                ToUUID:       similar.Memory.ID,
                RelationType: types.RelationCoTagged,
                Weight:       float32(entity.Confidence),
            })
        }
    }
}
```

### 2.6 Retry Strategy

| Attempt | Backoff | After |
|---------|---------|-------|
| 1 | - | Immediate |
| 2 | 5s | First retry |
| 3 | 15s | Second retry |
| 4 | 45s (last) | Third retry |

Sau 3 attempts → `status = 'failed'`, `error_message = err.Error()`.

### 2.7 Skip Conditions

Entity extraction bị skip nếu:
- `len(content) < 50` chars (too short for meaningful entities)
- `memory_type == "preference"` (preferences don't have entities)
- All previous attempts failed (max_attempts reached)

## 3. Auto-Link Worker

**File**: `internal/application/service/memory_v2/auto_link_worker.go`

### 3.1 Workflow

```
New Memory Saved
       │
       ▼
┌──────────────────┐
│ Check Co-Tagged  │──► Shared ≥2 important tags → relation
└──────────────────┘
       │
       ▼
┌──────────────────┐
│ Check Related    │──► Cosine >0.65 → relation
└──────────────────┘
       │
       ▼
┌──────────────────┐
│ Check Justifies  │──► decision → episodic/fact → relation
└──────────────────┘
```

### 3.2 Link Types

#### 3.2.1 Co-Tagged Links

```go
func (w *AutoLinkWorker) createCoTaggedLinks(ctx context.Context, memory *types.AgentMemory) {
    // Find memories sharing ≥2 tags with importance > threshold
    importantTags := filterImportantTags(memory.Tags)
    if len(importantTags) < 2 {
        return
    }
    
    similar, _ := w.repo.Search(ctx, &types.MemoryFilter{
        TenantID: memory.TenantID,
        Tags:     importantTags,
        Limit:    10,
    }, "", nil)
    
    for _, s := range similar {
        sharedTags := intersectTags(memory.Tags, s.Memory.Tags)
        if len(sharedTags) >= 2 {
            w.repo.SaveRelation(ctx, &types.MemoryRelation{
                FromUUID:     memory.ID,
                ToUUID:       s.Memory.ID,
                RelationType: types.RelationCoTagged,
                Weight:       float32(len(sharedTags)) / 10.0,
            })
        }
    }
}
```

#### 3.2.2 Related-To Links

```go
func (w *AutoLinkWorker) createRelatedLinks(ctx context.Context, memory *types.AgentMemory) {
    similar, _ := w.repo.FindSimilar(ctx, memory.TenantID, memory.Embedding, 0.65, 20)
    for _, s := range similar {
        if s.ID == memory.ID {
            continue
        }
        w.repo.SaveRelation(ctx, &types.MemoryRelation{
            FromUUID:     memory.ID,
            ToUUID:       s.ID,
            RelationType: types.RelationRelatedTo,
            Weight:       float32(cosineSimilarity(memory.Embedding, s.Embedding)),
        })
    }
}
```

#### 3.2.3 Justifies Links

```go
func (w *AutoLinkWorker) createJustifiesLinks(ctx context.Context, memory *types.AgentMemory) {
    if memory.MemoryType == types.MemoryTypeDecision {
        // Tìm fact/semantic memories liên quan
        related, _ := w.repo.Search(ctx, &types.MemoryFilter{
            TenantID:    memory.TenantID,
            MemoryTypes: []types.MemoryType{types.MemoryTypeFact, types.MemoryTypeSemantic},
            Limit:       5,
        }, memory.Content, memory.Embedding)
        
        for _, r := range related {
            w.repo.SaveRelation(ctx, &types.MemoryRelation{
                FromUUID:     memory.ID,
                ToUUID:       r.Memory.ID,
                RelationType: types.RelationJustifies,
                Weight:       0.8,
            })
        }
    }
}
```

## 4. Consolidation Scheduler

**File**: `internal/application/service/memory_v2/consolidator.go`

### 4.1 Schedule

```
Every 6 hours:
  1. Compute hub_score for all memories (graph centrality)
  2. Decay old memories (>1 year, -10% importance)
  3. Merge near-duplicates (cosine >0.93)
  4. (Skip prune — done by Pruner)
  
Optional: daily/weekly/monthly summary generation
```

### 4.2 Hub Score Computation

```go
func (s *Consolidator) computeHubScores(ctx context.Context) error {
    // Batch update hub_score using degree + avg edge weight
    _, err := s.db.Exec(ctx, `
        WITH degree_stats AS (
            SELECT 
                memory_id,
                COUNT(*) AS degree,
                AVG(weight) AS avg_weight
            FROM (
                SELECT from_uuid AS memory_id, weight FROM memory_relations WHERE deleted_at IS NULL
                UNION ALL
                SELECT to_uuid AS memory_id, weight FROM memory_relations WHERE deleted_at IS NULL
            ) all_edges
            GROUP BY memory_id
        )
        UPDATE agent_memories am
        SET hub_score = LN(1 + COALESCE(ds.degree, 0)) * COALESCE(ds.avg_weight, 0.0)
        FROM degree_stats ds
        WHERE am.id = ds.memory_id
    `)
    return err
}
```

### 4.3 Decay Logic

```go
func (s *Consolidator) decayOldMemories(ctx context.Context) error {
    threshold := time.Now().AddDate(-1, 0, 0) // 1 year ago
    
    memories, _ := s.repo.Search(ctx, &types.MemoryFilter{
        Tier: []types.MemoryTier{types.TierCore, types.TierStandard},
        Limit: 1000,
    }, "", nil)
    
    for _, m := range memories {
        if m.Memory.CreatedAt.Before(threshold) {
            // Decay importance by 10%
            newImportance := int(float64(m.Memory.Importance) * 0.9)
            if newImportance < ImportanceMin {
                newImportance = ImportanceMin
            }
            m.Memory.Importance = newImportance
            s.repo.Update(ctx, m.Memory)
        }
    }
    return nil
}
```

### 4.4 Near-Duplicate Merge

```go
func (s *Consolidator) mergeNearDuplicates(ctx context.Context) error {
    // Find pairs with cosine > 0.93
    // Merge content (concatenate, truncate at 2000 chars)
    // Keep the older one, soft-delete the newer
}
```

### 4.5 Optional Summary Generation

```go
func (s *Consolidator) generateSummary(ctx context.Context, period string) error {
    // period: "daily", "weekly", "monthly"
    // Query memories from period
    // LLM summary prompt → store as new semantic memory
    // Tag with "summary" + period
}
```

## 5. Pruner

**File**: `internal/application/service/memory_v2/pruner.go`

### 5.1 Schedule

```
Daily at 3:00 AM:
  1. Soft-delete expired memories
  2. Hard-delete tier-3 memories (soft-deleted >14d, access_count=0)
  3. Protect: tier-0, critical tag, permanent tag
```

### 5.2 Soft Delete (Daily)

```go
func (p *Pruner) softDeleteExpired(ctx context.Context) (int64, error) {
    now := time.Now()
    
    // Tier-1: expired AND not accessed in 30 days → soft delete
    // Tier-2: expired AND not accessed in 30 days → soft delete
    // Tier-3: past TTL → soft delete immediately
    
    count, err := p.repo.HardDeleteExpired(ctx, now, 0)
    return count, err
}
```

```sql
UPDATE agent_memories
SET deleted_at = NOW()
WHERE deleted_at IS NULL
  AND expires_at IS NOT NULL
  AND expires_at < NOW()
  AND tier > 0
  AND (
    tier < 3 AND last_accessed_at < NOW() - INTERVAL '30 days'
    OR tier = 3
  )
  AND NOT ('critical' = ANY(tags) OR 'permanent' = ANY(tags));
```

### 5.3 Hard Delete (Daily)

```go
func (p *Pruner) hardDeleteTier3(ctx context.Context) (int64, error) {
    // Tier-3 soft-deleted >14 days ago AND access_count = 0
    return p.repo.HardDeleteExpired(ctx, time.Now().AddDate(0, 0, -14), 14*24*time.Hour)
}
```

```sql
DELETE FROM agent_memories
WHERE deleted_at IS NOT NULL
  AND deleted_at < NOW() - INTERVAL '14 days'
  AND tier = 3
  AND access_count = 0;
```

### 5.4 Protected Memories

Các memory sau **KHÔNG BAO GIỜ** bị prune:
- `tier = 0` (Critical)
- Tags chứa `"critical"` hoặc `"permanent"`
- `importance >= 5`

## 6. Dreamer Worker

**File**: `internal/application/service/memory_v2/dreamer_worker.go`

The Dreamer is an REM-inspired consolidation worker that periodically "dreams" over memories to propose consolidations — merging duplicates, updating verdicts, adjusting importance, and pruning noise.

### 6.1 Gate System

Dreamer dùng gate system để tránh concurrent dreams và đảm bảo không chạy quá thường xuyên:

```go
type DreamerGate struct {
    db *gorm.DB
}

func (g *DreamerGate) TryLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
    result := g.db.Exec(ctx, `
        INSERT INTO dreamer_state (tenant_id, locked_by, locked_until, last_run_at)
        VALUES ($1, $2, NOW() + INTERVAL '10 minutes', NOW())
        ON CONFLICT (tenant_id) DO UPDATE
        SET locked_by = $2,
            locked_until = NOW() + INTERVAL '10 minutes',
            last_run_at = NOW()
        WHERE dreamer_state.locked_until IS NULL
           OR dreamer_state.locked_until < NOW()
    `, tenantID, workerID)
    
    return result.RowsAffected > 0, result.Error
}

func (g *DreamerGate) Unlock(ctx context.Context, tenantID string) error {
    return g.db.Exec(ctx, `
        UPDATE dreamer_state
        SET locked_by = NULL, locked_until = NULL, updated_at = NOW()
        WHERE tenant_id = $1
    `, tenantID).Error
}
```

### 6.2 Schedule

- **Interval**: Every 1 hour (minimum, gate-enforced)
- **Max duration per pass**: 5 minutes
- **Max actions per pass**: 5 (configurable)
- **Token budget per LLM call**: 4000 tokens
- **Model**: Cheap/fast (e.g., GPT-3.5-turbo, Ollama local model)

### 6.3 Four-Phase Prompt

Dreamer runs a single LLM call with a structured 4-phase prompt:

```
You are a memory consolidation agent. Analyze {N} memories and propose actions.

## Phase 1: Identify Redundancies
Find memory pairs with near-identical meaning. Suggest MERGE.

## Phase 2: Detect Contradictions
Find memories that contradict each other. Newer/verified info takes precedence.
Suggest UPDATE_VERDICT (refuted) for the wrong one.

## Phase 3: Adjust Importance
Find memories that are more important than their current score suggests.
Suggest BUMP_IMPORTANCE.

## Phase 4: Prune Noise
Find memories with no long-term value (greetings, trivial chat).
Suggest DELETE (soft-delete, tier-3 only).

## Memories to analyze:
{serialized_memories}

## Output format (JSON):
{
  "actions": [
    {"type": "merge", "target_ids": ["id1", "id2"], "reason": "...", "confidence": 0.85},
    {"type": "update_verdict", "target_id": "id3", "new_verdict": "refuted", "reason": "...", "confidence": 0.90},
    {"type": "bump_importance", "target_id": "id4", "delta": 2, "reason": "...", "confidence": 0.75},
    {"type": "delete", "target_id": "id5", "reason": "...", "confidence": 0.95}
  ]
}

## Constraints:
- Max 5 actions total
- Confidence must be >= 0.70 for any action
- Do NOT touch memories with verdict "decision" or "fixed" (protected)
- Do NOT delete tier-0 or tier-1 memories
- Only return JSON, no explanation text
```

### 6.4 Action Parser & Validator

```go
func (d *DreamerWorker) parseAndValidateActions(rawJSON string) ([]DreamAction, error) {
    var response struct {
        Actions []DreamAction `json:"actions"`
    }
    if err := json.Unmarshal([]byte(rawJSON), &response); err != nil {
        return nil, fmt.Errorf("dreamer: invalid JSON response: %w", err)
    }
    
    var validated []DreamAction
    for _, action := range response.Actions {
        if action.Confidence < 0.70 {
            continue // Skip low-confidence actions
        }
        
        // Check protected verdicts
        memory, _ := d.repo.GetByID(ctx, action.TargetID)
        if memory != nil && memory.Verdict.IsProtected() {
            continue // Never touch protected memories
        }
        
        // Check tier constraints
        if action.Type == "delete" && memory != nil && memory.Tier < 2 {
            continue // Don't delete high-tier memories
        }
        
        validated = append(validated, action)
    }
    
    // Cap at max actions
    if len(validated) > d.maxActions {
        validated = validated[:d.maxActions]
    }
    
    return validated, nil
}
```

### 6.5 Budget Control

```go
func (d *DreamerWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            tenants, _ := d.repo.ListActiveTenants(ctx)
            for _, tenantID := range tenants {
                // Gate check: has it been >1h since last run?
                ok, _ := d.gate.TryLock(ctx, tenantID, d.workerID)
                if !ok {
                    continue // Another worker is dreaming, or ran recently
                }
                defer d.gate.Unlock(ctx, tenantID)
                
                // Run with hard budget
                result, err := d.dream(ctx, tenantID)
                if err != nil {
                    log.Warnf("dreamer: tenant %s failed: %v", tenantID, err)
                    continue
                }
                
                log.Infof("dreamer: tenant %s: %d actions proposed, %d applied, %d tokens",
                    tenantID, result.ActionsProposed, result.ActionsApplied, result.TokenUsed)
            }
        }
    }
}
```

## 7. HealthChecker Worker

**File**: `internal/application/service/memory_v2/health_checker.go`

### 7.1 Schedule

Daily at 4:00 AM, runs 6 health checks and produces a report.

### 7.2 Six Health Checks

```go
func (h *HealthChecker) Run(ctx context.Context) ([]*MemoryHealthIssue, error) {
    var issues []*MemoryHealthIssue
    
    // Check 1: Orphan detection
    issues = append(issues, h.checkOrphans(ctx)...)
    
    // Check 2: Stale fact detection
    issues = append(issues, h.checkStaleFacts(ctx)...)
    
    // Check 3: Contradiction scan
    issues = append(issues, h.checkContradictions(ctx)...)
    
    // Check 4: Duplication scan
    issues = append(issues, h.checkDuplication(ctx)...)
    
    // Check 5: Graph fragmentation
    issues = append(issues, h.checkGraphFragmentation(ctx)...)
    
    // Check 6: Verdict consistency
    issues = append(issues, h.checkVerdictConsistency(ctx)...)
    
    return issues, nil
}
```

| # | Check | Description | Query |
|---|-------|-------------|-------|
| 1 | Orphan detection | Memories with 0 tags AND 0 relations | `WHERE array_length(tags, 1) IS NULL AND id NOT IN (SELECT from_uuid FROM memory_relations UNION SELECT to_uuid FROM memory_relations)` |
| 2 | Stale fact detection | Memories >180 days, importance <1, not accessed >90d | `WHERE created_at < NOW() - INTERVAL '180 days' AND importance < 1 AND last_accessed_at < NOW() - INTERVAL '90 days'` |
| 3 | Contradiction scan | High cosine similarity (>0.85) + opposite sentiment → potential contradiction | Pairwise cosine check on top 1000 memories |
| 4 | Duplication scan | Fingerprint collisions OR cosine >0.95 pairs | Fingerprint GROUP BY + cosine scan |
| 5 | Graph fragmentation | Ratio of isolated nodes / total | `COUNT(*) FILTER (WHERE hub_score = 0) / COUNT(*)::float > 0.5` |
| 6 | Verdict consistency | WIP memories >30 days → suggest update | `WHERE verdict = 'wip' AND created_at < NOW() - INTERVAL '30 days'` |

### 7.3 Report Format

```go
type HealthReport struct {
    TenantID    string                `json:"tenant_id"`
    CheckedAt   time.Time             `json:"checked_at"`
    TotalIssues int                   `json:"total_issues"`
    BySeverity  map[string]int        `json:"by_severity"`
    Issues      []*MemoryHealthIssue  `json:"issues"`
}
```

## 8. CacheWarmer (Optional)

**File**: `internal/application/service/memory_v2/cache_warmer.go`

### 8.1 Purpose

Pre-warm embedding cache cho frequent queries để giảm cold-start latency.

```go
func (w *CacheWarmer) Run(ctx context.Context) {
    // On startup: warm top 100 frequent queries
    topQueries := w.getTopQueries(ctx, 100)
    for _, q := range topQueries {
        embedding, _ := w.embedder.Embed(ctx, q)
        w.cache.Set("embed:"+q, embedding, 5*time.Minute)
    }
    
    // Every 30 minutes: refresh top queries
    ticker := time.NewTicker(30 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.warmTopQueries(ctx)
        }
    }
}
```

### 8.2 Top Queries Source

- Query from `access_log` or query frequency counter
- Fallback: most recent successful search queries

## 9. Worker Lifecycle

### 9.1 Startup

```go
func (s *MemoryServiceV2Impl) StartWorkers(ctx context.Context) {
    // Entity Extractor (batch mode)
    go s.entityExtractor.Run(ctx)
    
    // Consolidation Scheduler (includes hub_score)
    go s.consolidator.Run(ctx)
    
    // Dreamer Worker (LLM consolidation)
    go s.dreamer.Run(ctx)
    
    // Pruner
    go s.pruner.Run(ctx)
    
    // HealthChecker (daily)
    go s.healthChecker.Run(ctx)
    
    // CacheWarmer (optional)
    if s.config.CacheWarmer.Enabled {
        go s.cacheWarmer.Run(ctx)
    }
    
    // Auto-Link is triggered per-memory, not a continuous worker
}
```

### 9.2 Graceful Shutdown

```go
func (s *MemoryServiceV2Impl) Cleanup(ctx context.Context) error {
    s.cancel() // signal all workers to stop
    s.wg.Wait() // wait for workers to finish current job
    return nil
}
```

### 9.3 Worker Pool

Sử dụng `ants` goroutine pool để giới hạn concurrent workers:

```go
pool, _ := ants.NewPool(5)
defer pool.Release()

pool.Submit(func() {
    w.processJob(ctx, job)
})
```

## 10. Monitoring

| Metric | Source | Alert |
|--------|--------|-------|
| `extraction_queue_depth` | COUNT pending jobs | >100 pending |
| `extraction_failure_rate` | failed / total jobs | >10% |
| `extraction_batch_size_avg` | items per batch | <3 (inefficient) |
| `prune_deleted_count` | Daily prune count | >10000 (unusual) |
| `worker_restart_count` | Worker panic recovery | >3/hour |
| `consolidation_duration` | Time to complete | >5min |
| `dream_pass_count` | Total dream passes | Monitor trend |
| `dream_actions_proposed` | Actions per pass | >10 (unusual) |
| `dream_actions_applied` | Actions actually applied | < proposed by >50% (gate too strict?) |
| `dream_token_used` | Tokens per dream pass | >budget (4000) |
| `dream_failure_count` | Failed dream passes | >3/hour |
| `health_issues_total` | Total health issues found | >100 (unusual) |
| `health_issues_critical` | Critical health issues | >0 |
| `cache_hit_rate` | Embedding + result cache hit | <50% (cache ineffective) |
