# 02 — API & Interface Reference

> Memory v2 Module | Last Update: 2026-07-09

## 1. MemoryServiceV2 Interface

```go
// File: internal/types/interfaces/memory_v2.go

type MemoryServiceV2 interface {
    // === Backward Compat (implements existing MemoryService) ===
    
    // AddEpisode processes a conversation and stores it as a memory episode.
    AddEpisode(ctx context.Context, userID string, sessionID string, messages []types.Message) error
    
    // RetrieveMemory retrieves relevant memory context for a query.
    RetrieveMemory(ctx context.Context, userID string, query string) (*types.MemoryContext, error)
    
    // === New Methods ===
    
    // SaveMemory saves a single memory entry with full ingestion pipeline.
    // Returns SaveMemoryResult with lint issues if any.
    SaveMemory(ctx context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error)
    
    // SearchMemories performs hybrid search across all memories.
    SearchMemories(ctx context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error)
    
    // GetMemory retrieves a single memory by ID.
    GetMemory(ctx context.Context, tenantID string, memoryID string) (*types.AgentMemory, error)
    
    // UpdateMemory updates an existing memory.
    UpdateMemory(ctx context.Context, memory *types.AgentMemory) error
    
    // DeleteMemory soft-deletes a memory.
    DeleteMemory(ctx context.Context, tenantID string, memoryID string) error
    
    // RestoreMemory restores a soft-deleted memory.
    RestoreMemory(ctx context.Context, tenantID string, memoryID string) error
    
    // BoostImportance increases a memory's importance score.
    BoostImportance(ctx context.Context, tenantID string, memoryID string, delta int) error
    
    // LinkMemories creates a relation between two memories.
    LinkMemories(ctx context.Context, relation *types.MemoryRelation) error
    
    // UnlinkMemories removes a relation between two memories.
    UnlinkMemories(ctx context.Context, tenantID string, relationID string) error
    
    // GetMemoryGraph traverses the relation graph from a memory.
    GetMemoryGraph(ctx context.Context, tenantID string, memoryID string, depth int) ([]*types.MemoryRelation, error)
    
    // GetStats returns aggregate statistics for a tenant.
    GetStats(ctx context.Context, tenantID string) (*types.MemoryStats, error)
    
    // ConsolidateDream runs one dreamer pass: analyzes memories and proposes
    // consolidations (merge, update verdict, bump importance, etc.).
    ConsolidateDream(ctx context.Context, tenantID string) (*types.DreamResult, error)
    
    // AssessHealth runs health checks: orphan detection, stale facts,
    // contradictions, duplication, graph fragmentation.
    AssessHealth(ctx context.Context, tenantID string) ([]*types.MemoryHealthIssue, error)
    
    // Cleanup performs maintenance: hard-delete expired, vacuum.
    Cleanup(ctx context.Context) error
}
```

## 2. MemoryRepositoryV2 Interface

```go
// File: internal/types/interfaces/memory_v2.go

type MemoryRepositoryV2 interface {
    // === CRUD ===
    
    Save(ctx context.Context, memory *types.AgentMemory) error
    GetByID(ctx context.Context, tenantID string, memoryID string) (*types.AgentMemory, error)
    Update(ctx context.Context, memory *types.AgentMemory) error
    SoftDelete(ctx context.Context, tenantID string, memoryID string) error
    HardDelete(ctx context.Context, tenantID string, memoryID string) error
    HardDeleteExpired(ctx context.Context, before time.Time, minAge time.Duration) (int64, error)
    
    // === Search ===
    
    // Search performs hybrid search (BM25 + cosine + graph).
    Search(ctx context.Context, filter *types.MemoryFilter, query string, embedding []float32) ([]*types.MemorySearchResult, error)
    
    // BM25Search performs full-text search only.
    BM25Search(ctx context.Context, filter *types.MemoryFilter, query string) ([]*types.MemorySearchResult, error)
    
    // CosineSearch performs vector similarity search only.
    CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)
    
    // === Graph ===
    
    SaveRelation(ctx context.Context, relation *types.MemoryRelation) error
    DeleteRelation(ctx context.Context, tenantID string, relationID string) error
    FindRelations(ctx context.Context, tenantID string, memoryID string, depth int) ([]*types.MemoryRelation, error)
    FindRelationsByType(ctx context.Context, tenantID string, memoryID string, relType types.RelationType) ([]*types.MemoryRelation, error)
    
    // === Dedup ===
    
    FindByFingerprint(ctx context.Context, tenantID string, fingerprint string) (*types.AgentMemory, error)
    FindSimilar(ctx context.Context, tenantID string, embedding []float32, threshold float64, limit int) ([]*types.AgentMemory, error)
    
    // === Queue ===
    
    EnqueueExtraction(ctx context.Context, job *types.ExtractionJob) error
    DequeueExtraction(ctx context.Context, limit int) ([]*types.ExtractionJob, error)
    MarkExtractionDone(ctx context.Context, jobID string) error
    MarkExtractionFailed(ctx context.Context, jobID string, errMsg string) error
    PurgeFailedExtractions(ctx context.Context, maxAttempts int) (int64, error)
    
    // === Stats ===
    
    GetStats(ctx context.Context, tenantID string) (*types.MemoryStats, error)
    
    // === Bulk ===
    
    BulkUpdateTier(ctx context.Context, memories []*types.AgentMemory) error
    BulkSave(ctx context.Context, memories []*types.AgentMemory) error
    
    // === Maintenance ===
    
    Vacuum(ctx context.Context) error
}
```

## 3. Domain Types

### MemoryVerdict

```go
type MemoryVerdict string

const (
    VerdictNone     MemoryVerdict = "none"
    VerdictFixed    MemoryVerdict = "fixed"
    VerdictRefuted  MemoryVerdict = "refuted"
    VerdictDecision MemoryVerdict = "decision"
    VerdictGotcha   MemoryVerdict = "gotcha"
    VerdictWIP      MemoryVerdict = "wip"
)

// IsProtected returns true if verdict cannot be auto-downgraded by LLM
func (v MemoryVerdict) IsProtected() bool {
    return v == VerdictDecision || v == VerdictFixed
}
```

### AgentMemory

> ⚠ **Code gap**: Fields `Verdict`, `HubScore` are added by migration 000065 (planned).
> Current code at `internal/types/memory_v2.go:112` does NOT include these fields yet.

```go
type AgentMemory struct {
    ID             string              `json:"id"`
    TenantID       string              `json:"tenant_id"`
    KbID           string              `json:"kb_id"`
    UserID         string              `json:"user_id"`
    SessionID      string              `json:"session_id"`
    Content        string              `json:"content"`
    MemoryType     MemoryType          `json:"memory_type"`
    Importance     int                 `json:"importance"`
    Tier           MemoryTier          `json:"tier"`
    Embedding      []float32           `json:"embedding,omitempty"`
    Fingerprint    string              `json:"fingerprint,omitempty"`
    Verdict        MemoryVerdict       `json:"verdict"`        // ⚠ 000065
    HubScore       float64             `json:"hub_score"`      // ⚠ 000065
    Tags           []string            `json:"tags,omitempty"`
    Metadata       map[string]string   `json:"metadata,omitempty"`
    AccessCount    int64               `json:"access_count"`
    LastAccessedAt *time.Time          `json:"last_accessed_at,omitempty"`
    CreatedAt      time.Time           `json:"created_at"`
    UpdatedAt      time.Time           `json:"updated_at"`
    DeletedAt      *time.Time          `json:"deleted_at,omitempty"`
    ExpiresAt      *time.Time          `json:"expires_at,omitempty"`
}
```

### MemoryFilter

> ⚠ **Code gap**: Fields `SessionID`, `Verdicts`, `MinScore`, `DeepGraph` not in current code (`memory_v2.go:259`).

```go
type MemoryFilter struct {
    TenantID       string          `json:"tenant_id"`
    KbID           string          `json:"kb_id,omitempty"`
    UserID         string          `json:"user_id,omitempty"`
    SessionID      string          `json:"session_id,omitempty"`
    MemoryTypes    []MemoryType    `json:"memory_types,omitempty"`
    Verdicts       []MemoryVerdict `json:"verdicts,omitempty"`
    Tiers          []MemoryTier    `json:"tiers,omitempty"`
    Tags           []string        `json:"tags,omitempty"`
    ImportanceMin  int             `json:"importance_min,omitempty"`
    ImportanceMax  int             `json:"importance_max,omitempty"`
    MinScore       float64         `json:"min_score,omitempty"`
    Limit          int             `json:"limit,omitempty"`
    Offset         int             `json:"offset,omitempty"`
    DeepGraph      bool            `json:"deep_graph,omitempty"`
    IncludeDeleted bool            `json:"include_deleted,omitempty"`
}
```

### MemorySearchResult

> ⚠ **Code gap**: Fields `IsStale`, `StaleDays` not in current code (`memory_v2.go:285`).

```go
type MemorySearchResult struct {
    Memory          *AgentMemory `json:"memory"`
    BM25Score       float64      `json:"bm25_score"`
    CosineScore     float64      `json:"cosine_score"`
    GraphScore      float64      `json:"graph_score"`
    ImportanceScore float64      `json:"importance_score"`
    FinalScore      float64      `json:"final_score"`
    IsStale         bool         `json:"is_stale"`    // ⚠ Planned
    StaleDays       int          `json:"stale_days"`  // ⚠ Planned
}
```

### MemoryStats

> ⚠ **Code gap**: Field `ByVerdict` not in current code (`memory_v2.go:299`). Basic `MemoryStats` exists.

```go
type MemoryStats struct {
    Total         int64                  `json:"total"`
    ByType        map[MemoryType]int64   `json:"by_type"`
    ByTier        map[MemoryTier]int64   `json:"by_tier"`
    ByVerdict     map[MemoryVerdict]int64 `json:"by_verdict"`
    AvgImportance float64                `json:"avg_importance"`
    TotalAccesses int64                  `json:"total_accesses"`
    LastUpdated   time.Time              `json:"last_updated"`
}
```

### MemoryLintIssue

```go
type MemoryLintIssue struct {
    Rule     string `json:"rule"`      // e.g. "stale_fact", "contradiction"
    Severity string `json:"severity"`   // "warning" | "error"
    Message  string `json:"message"`    // Human-readable description
    SourceID string `json:"source_id"`  // Conflicting memory ID (if applicable)
}
```

### SaveMemoryResult

```go
type SaveMemoryResult struct {
    Memory     *AgentMemory       `json:"memory"`
    Created    bool               `json:"created"`     // false nếu dedup merged
    LintIssues []MemoryLintIssue  `json:"lint_issues"` // Lint-on-write results
}
```

### MemoryHealthIssue

```go
type MemoryHealthIssue struct {
    Type        string  `json:"type"`        // e.g. "orphan", "stale", "contradiction"
    MemoryID    string  `json:"memory_id"`
    Description string  `json:"description"`
    Severity    string  `json:"severity"`     // "low" | "medium" | "high" | "critical"
    Suggestion  string  `json:"suggestion"`   // Recommended action
}
```

### TokenBudgetConfig

```go
type TokenBudgetConfig struct {
    MaxTotalTokens    int     `json:"max_total_tokens"`     // Max tokens for memory context (default: 2000)
    Mode              string  `json:"mode"`                  // "full" | "truncated" | "summary"
    TruncateThreshold int     `json:"truncate_threshold"`    // Switch to truncated mode (default: 1500)
    SummaryThreshold  int     `json:"summary_threshold"`     // Switch to summary mode (default: 2500)
    MaxPerMemory      int     `json:"max_per_memory"`        // Max tokens per memory entry (default: 300)
    ReserveForSystem  int     `json:"reserve_for_system"`    // Tokens reserved for system prompt (default: 500)
}

func DefaultTokenBudget() TokenBudgetConfig {
    return TokenBudgetConfig{
        MaxTotalTokens:    2000,
        Mode:              "full",
        TruncateThreshold: 1500,
        SummaryThreshold:  2500,
        MaxPerMemory:      300,
        ReserveForSystem:  500,
    }
}
```

### DreamerConfig

```go
type DreamerConfig struct {
    Enabled      bool          `json:"enabled"`
    Interval     time.Duration `json:"interval"`      // Min time between dream passes (default: 1h)
    ModelID      string        `json:"model_id"`      // Cheap/fast model for consolidation
    MaxActions   int           `json:"max_actions"`   // Max actions per dream pass (default: 5)
    TokenBudget  int           `json:"token_budget"`  // Max tokens per dream LLM call (default: 4000)
    DryRun       bool          `json:"dry_run"`       // Preview mode, no mutations
}

// DreamResult represents the outcome of a dreamer consolidation pass
type DreamResult struct {
    ActionsProposed int              `json:"actions_proposed"`
    ActionsApplied  int              `json:"actions_applied"`
    Actions         []DreamAction    `json:"actions"`
    TokenUsed       int              `json:"token_used"`
    Duration        time.Duration    `json:"duration"`
}

type DreamAction struct {
    Type        string  `json:"type"`        // "merge" | "update_verdict" | "bump_importance" | "delete"
    TargetID    string  `json:"target_id"`   // Memory ID
    TargetIDs   []string `json:"target_ids,omitempty"` // For merge actions
    NewVerdict  string  `json:"new_verdict,omitempty"` // For update_verdict
    Delta       int     `json:"delta,omitempty"`       // For bump_importance
    Reason      string  `json:"reason"`      // Why this action was proposed
    Confidence  float64 `json:"confidence"`  // LLM confidence 0.0-1.0
    Applied     bool    `json:"applied"`     // false in dry-run mode
}

// TokenBudgetInfo represents the current token budget state during retrieval
type TokenBudgetInfo struct {
    Mode      string `json:"mode"`      // "full" | "truncated" | "summary"
    Used      int    `json:"used"`
    Remaining int    `json:"remaining"`
}

// CacheWarmerConfig controls the optional cache warming behavior
type CacheWarmerConfig struct {
    Enabled       bool          `json:"enabled"`
    TopQueriesN   int           `json:"top_queries_n"`   // Default: 100
    RefreshInterval time.Duration `json:"refresh_interval"` // Default: 30m
}

// MemoryV2Config combines all Memory v2 configuration
// ⚠ Code gap: Current code (memory_v2.go:371) has a smaller MemoryV2Config.
// The struct below is the TARGET state. Missing in code:
//   - SemanticDedup (DedupConfig)
//   - RecencyBoost (RecencyBoostConfig)  
//   - TokenBudget (TokenBudgetConfig)
//   - Dreamer (DreamerConfig)
//   - CacheWarmer (CacheWarmerConfig)
//   - LintOnWrite (LintOnWriteConfig)
//   - MinScoreThreshold
//   - HNSW params (m, ef_construction, ef_search)
type MemoryV2Config struct {
    MaxMemoryContentLength int               `json:"max_memory_content_length"`
    EmbeddingDimensions    int               `json:"embedding_dimensions"`
    SemanticDedup          DedupConfig       `json:"semantic_dedup"`
    HybridSearchWeights    HybridSearchWeights `json:"hybrid_search_weights"`
    SearchBackend          string            `json:"search_backend"`
    MaxSearchResults       int               `json:"max_search_results"`
    RecencyBoost           RecencyBoostConfig `json:"recency_boost"`
    TokenBudget            TokenBudgetConfig `json:"token_budget"`
    Dreamer                DreamerConfig     `json:"dreamer"`
    CacheWarmer            CacheWarmerConfig `json:"cache_warmer"`
    LintOnWrite            LintOnWriteConfig `json:"lint_on_write"`
}

type DedupConfig struct {
    ExactThreshold float64 `json:"exact_threshold"`
    NearThreshold  float64 `json:"near_threshold"`
    MaxMerges      int     `json:"max_merges"`
    MergeMaxChars  int     `json:"merge_max_chars"`
}

type LintOnWriteConfig struct {
    Enabled                 bool    `json:"enabled"`
    StaleThresholdDays      int     `json:"stale_threshold_days"`
    ContradictionThreshold  float64 `json:"contradiction_threshold"`
    NearDuplicateThreshold  float64 `json:"near_duplicate_threshold"`
}

type RecencyBoostConfig struct {
    Enabled              bool          `json:"enabled"`
    ShortTermMultiplier  float64       `json:"short_term_multiplier"`
    ShortTermWindow      time.Duration `json:"short_term_window"`
    LongTermFactor       float64       `json:"long_term_factor"`
    LongTermHalfLifeDays int           `json:"long_term_half_life_days"`
}

type HybridSearchWeights struct {
    BM25       float64 `json:"bm25"`
    Cosine     float64 `json:"cosine"`
    Graph      float64 `json:"graph"`
    Importance float64 `json:"importance"`
}
```

## 4. MemoryContextV2 (Output Format)

### MemoryContextV2 Type

```go
// MemoryContextV2 extends MemoryContext with structured v2 metadata.
// Exists in internal/types/memory_v2.go — used internally, not exposed via interface.
type MemoryContextV2 struct {
    Episodes  []Episode              `json:"episodes"`
    Entities  []Entity               `json:"entities"`
    Relations []Relationship         `json:"relations"`
    Results   []*MemorySearchResult  `json:"results"`
    Formatted string                 `json:"formatted"`  // Structured XML
    Stats     *MemoryStats           `json:"stats,omitempty"`
}
```

### XML Output Format

Khi `RetrieveMemory` được gọi, kết quả được format thành structured XML context:

```xml
<memory_context>
  <query>cách deploy service mới lên production?</query>
  
  <results>
    <memory id="abc-123" type="procedural" importance="4" tier="1" score="0.87"
            verdict="fixed" stale_days="3" hub_score="1.2">
      Deploy service mới: build Docker image → push registry → kubectl apply -f deployment.yaml
    </memory>
    <memory id="def-456" type="decision" importance="3" tier="1" score="0.72"
            verdict="decision" stale_days="45" hub_score="0.8">
      Quyết định dùng Kubernetes thay vì Docker Swarm vì ecosystem tốt hơn
    </memory>
    <memory id="ghi-789" type="semantic" importance="2" tier="2" score="0.55"
            verdict="none" stale_days="10" hub_score="0.3">
      Production cluster chạy trên EKS với 3 node groups
    </memory>
  </results>
  
  <stats total="3" by_type="procedural:1,decision:1,semantic:1" />
  <token_budget used="450" remaining="1550" mode="full" />
</memory_context>
```

## 5. Error Handling

Tất cả methods return error với các loại:

| Error | HTTP Status | Mô tả |
|-------|-------------|-------|
| `ErrMemoryNotFound` | 404 | Memory không tồn tại |
| `ErrMemoryValidation` | 400 | Content validation failed |
| `ErrMemoryDuplicate` | 409 | Duplicate content detected |
| `ErrMemoryUnavailable` | 503 | Repository not available |
| `ErrExtractionFailed` | 500 | Entity extraction failed after retries |

## 6. Usage Examples

### Lưu memory

```go
svc := memoryV2Service.NewMemoryServiceV2(repo, embedder, chatModel)

memory := &types.AgentMemory{
    TenantID: "tenant-1",
    KbID:     "kb-1",
    UserID:   "user-1",
    Content:  "Hôm nay tôi đã deploy service X lên production bằng lệnh kubectl apply",
}
result, err := svc.SaveMemory(ctx, memory)
if err != nil {
    log.Errorf("save failed: %v", err)
    return
}
if !result.Created {
    log.Infof("memory merged with existing: %s", result.Memory.ID)
}
for _, issue := range result.LintIssues {
    log.Warnf("lint [%s] %s: %s", issue.Severity, issue.Rule, issue.Message)
}
```

### Tìm kiếm

```go
filter := &types.MemoryFilter{
    TenantID:    "tenant-1",
    MemoryTypes: []types.MemoryType{types.MemoryTypeProcedural, types.MemoryTypeDecision},
    Limit:       10,
}
results, err := svc.SearchMemories(ctx, "deploy production", filter)
```

### Lấy memory context cho chat

```go
memoryCtx, err := svc.RetrieveMemory(ctx, "user-1", "cách deploy service mới?")
// memoryCtx.RelatedEpisodes[0].Summary chứa structured XML context
```

## 7. System Integration

### 7.1 DI Container Strategy

MemoryServiceV2 và MemoryService (Neo4j) cùng implement `interfaces.MemoryService`. Container dùng pattern factory để phân biệt:

```go
// internal/container/container.go

// Register both implementations with named tags
must(container.Provide(memoryService.NewMemoryService, dig.Name("neo4j")))
must(container.Provide(memoryV2Service.NewMemoryServiceV2, dig.Name("v2")))

// Factory: resolve based on env var
must(container.Provide(func(in struct {
    dig.In
    Neo4j interfaces.MemoryService `name:"neo4j"`
    V2    interfaces.MemoryService `name:"v2"`
}) interfaces.MemoryService {
    if os.Getenv("MEMORY_BACKEND") == "v2" {
        return in.V2
    }
    return in.Neo4j
}))
```

**Switch mechanism**: Set `MEMORY_BACKEND=v2` → restart service. Không cần code change. Cả 2 database cùng tồn tại không conflict.

### 7.2 MemoryContext Bridging

`interfaces.MemoryService.RetrieveMemory()` trả về `*types.MemoryContext`. V2 map structured XML context vào format plugin hiểu được — **không cần sửa plugin code**:

```go
func (s *MemoryServiceV2Impl) RetrieveMemory(
    ctx context.Context, userID string, query string,
) (*types.MemoryContext, error) {
    results, err := s.RetrieveMemoryV2(ctx, userID, query)
    if err != nil {
        return nil, err
    }
    formatted := s.packContext(query, results)

    // Bridge: embed formatted XML as single episode Summary
    return &types.MemoryContext{
        RelatedEpisodes: []types.Episode{{
            UserID:    userID,
            Summary:   formatted,
            CreatedAt: time.Now(),
        }},
    }, nil
}
```

**Tại sao không cần sửa plugin**: `chat_pipeline/memory.go:74-79` lặp `RelatedEpisodes`, append `Summary` vào `UserContent`. V2 đặt toàn bộ XML vào 1 episode → plugin hoạt động nguyên trạng.

**Tại sao không dùng MemoryContextV2 trực tiếp**: `MemoryContextV2` extend `MemoryContext` nhưng plugin chỉ inject `interfaces.MemoryService` (trả về `*MemoryContext`). Bridge là cách đơn giản nhất để giữ nguyên interface contract.

### 7.3 REST API Endpoints

Cho UI integration và quản trị. Tất cả endpoints require JWT auth + RBAC:

| Method | Endpoint | Handler | Purpose |
|--------|----------|---------|---------|
| `GET` | `/api/v1/memories` | `ListMemories` | `?kb_id=X&type=procedural&verdict=decision&page=1&limit=20` |
| `GET` | `/api/v1/memories/:id` | `GetMemory` | Single memory detail |
| `POST` | `/api/v1/memories` | `CreateMemory` | Manual memory creation |
| `PUT` | `/api/v1/memories/:id` | `UpdateMemory` | Update content/verdict/tags |
| `DELETE` | `/api/v1/memories/:id` | `DeleteMemory` | Soft-delete |
| `GET` | `/api/v1/memories/search` | `SearchMemories` | `?q=deploy&type=procedural&limit=20` |
| `GET` | `/api/v1/memories/graph/:id` | `GetMemoryGraph` | `?depth=2` → nodes + edges |
| `GET` | `/api/v1/memories/stats` | `GetMemoryStats` | Aggregate stats per tenant |
| `GET` | `/api/v1/memories/health` | `GetHealthReport` | Latest HealthChecker report |
| `POST` | `/api/v1/memories/dream` | `TriggerDream` | Manual dreamer pass (admin) |
| `GET` | `/api/v1/tenants/memory-status` | `MemoryStatus` | Backend type + availability |

**File**: `internal/handler/memory_v2.go` (new)

### 7.4 Frontend Compatibility

**Chat flow**: Không cần thay đổi — bridging approach giữ nguyên format plugin.

**Memory toggle (GeneralSettings.vue)**: Frontend hiện kiểm tra `isNeo4jAvailable`. Với V2, cần API mới:

```json
// GET /api/v1/tenants/memory-status
{
    "backend": "v2",
    "available": true,
    "neo4j_available": false,
    "memory_count": 1234
}
```

Thay đổi frontend tối thiểu: `isNeo4jAvailable` → `memoryBackendAvailable` (1 dòng Vue).

**Memory Browser (Phase 2)**: Tận dụng REST API cho UI quản lý memory:
- Memory list với filter đa chiều
- Graph visualization (D3.js/Cytoscape)
- Dreamer action review queue
- Health dashboard

### 7.5 Agent Engine Interaction

Agent engine (`internal/agent/engine.go`) có `memoryConsolidator` riêng — đây là in-session token management (không liên quan đến Memory v2). Hai system hoạt động độc lập:

| | Agent Consolidator | Memory v2 |
|---|---|---|
| Scope | In-session | Cross-session, persistent |
| Storage | Memory (in-process) | PostgreSQL |
| Trigger | When context > max tokens | On every chat |
| Output | System message "Memory Summary" | Structured XML context |

### 7.6 Migration & Coexistence

```
Production DB hiện tại:
  agent_memories, memory_relations, extraction_queue (000064 — đã có)

Deploy:
  1. migrate up 000065 → ADD COLUMN verdict, hub_score + CREATE dreamer_state
  2. migrate up 000066 → HNSW index replacement
  3. MEMORY_BACKEND=v2 → restart

Rollback:
  1. MEMORY_BACKEND=neo4j → restart
  2. (Optional) migrate down 000066, 000065
```

Cả 2 backend cùng tồn tại trên 1 DB — không conflict vì Neo4j backend không dùng `agent_memories` table.
