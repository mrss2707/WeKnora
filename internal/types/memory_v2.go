package types

import (
	"database/sql/driver"
	"math"
	"time"

	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Verdict type
// ---------------------------------------------------------------------------

// MemoryVerdict tells whether a memory is endorsed, refuted, etc.
type MemoryVerdict string

const (
	VerdictNone     MemoryVerdict = "none"
	VerdictFixed    MemoryVerdict = "fixed"
	VerdictRefuted  MemoryVerdict = "refuted"
	VerdictDecision MemoryVerdict = "decision"
	VerdictGotcha   MemoryVerdict = "gotcha"
	VerdictWIP      MemoryVerdict = "wip"
)

// IsProtected returns true for verdicts that cannot be changed automatically by LLM.
func (v MemoryVerdict) IsProtected() bool {
	return v == VerdictDecision || v == VerdictFixed
}

// MemoryEmbeddingDim is the fixed dimensionality of the agent_memories.embedding
// column (migration 900074 declares vector(MemoryEmbeddingDim)). pgvector
// rejects ivfflat/hnsw indexes above 2000 dims, so the column is capped here.
// The repository zero-pads every written/queried embedding from models with
// ≤MemoryEmbeddingDim dims to exactly this width — pgvector typmods require an
// exact match, and padding is cosine/L2-invariant. Larger models are rejected.
const MemoryEmbeddingDim = 2000

// ---------------------------------------------------------------------------
// Custom types
// ---------------------------------------------------------------------------

// TagsArray wraps []string for PostgreSQL text[] scanning via lib/pq.
type TagsArray []string

// Scan implements the sql.Scanner interface.
func (t *TagsArray) Scan(src interface{}) error {
	return (*pq.StringArray)(t).Scan(src)
}

// Value implements the driver.Valuer interface.
func (t TagsArray) Value() (driver.Value, error) {
	return pq.StringArray(t).Value()
}

// ---------------------------------------------------------------------------
// Memory context types (for agent ↔ memory bridge)
// ---------------------------------------------------------------------------

// Episode represents a conversation episode or a distinct interaction event.
type Episode struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryContext represents the retrieved memory context for a conversation.
type MemoryContext struct {
	RelatedEpisodes  []Episode      `json:"related_episodes"`
	RelatedEntities  []Entity       `json:"related_entities"`
	RelatedRelations []Relationship `json:"related_relations"`
}

// ---------------------------------------------------------------------------
// GORM models
// ---------------------------------------------------------------------------

// AgentMemory represents a single memory record stored in agent_memories.
type AgentMemory struct {
	ID         string        `gorm:"column:id;type:varchar(36);primaryKey;default:uuid_generate_v4()" json:"id"`
	TenantID   string        `gorm:"column:tenant_id;type:varchar(36);not null;index:idx_agent_memories_tenant" json:"tenant_id"`
	KbID       string        `gorm:"column:kb_id;type:varchar(36);not null" json:"kb_id"`
	UserID     string        `gorm:"column:user_id;type:varchar(36);not null;default:''" json:"user_id"`
	Content    string        `gorm:"column:content;type:text;not null" json:"content"`
	MemoryType string        `gorm:"column:memory_type;type:varchar(32);default:''" json:"memory_type"`
	Importance int           `gorm:"column:importance;type:int;default:0" json:"importance"`
	Tier       int           `gorm:"column:tier;type:int;default:2" json:"tier"`
	Verdict    MemoryVerdict `gorm:"column:verdict;type:varchar(16);default:none" json:"verdict"`
	HubScore   float64       `gorm:"column:hub_score;default:0" json:"hub_score"`
	// Embedding and Metadata are internal: never serialized to API clients
	// (a 2000-dim vector per row, and raw JSONB bytes would render as base64).
	Embedding      pgvector.Vector `gorm:"column:embedding;type:vector(2000)" json:"-"`
	AccessCount    int             `gorm:"column:access_count;type:int;default:0" json:"access_count"`
	SessionID      string          `gorm:"column:session_id;type:varchar(36);default:'';index:idx_agent_memories_session" json:"session_id"`
	Fingerprint    *string         `gorm:"column:fingerprint;type:varchar(64);index:idx_agent_memories_fingerprint,where:fingerprint IS NOT NULL AND deleted_at IS NULL" json:"fingerprint,omitempty"`
	Tags           TagsArray       `gorm:"column:tags;type:text[];default:'{}'" json:"tags,omitempty"`
	Metadata       []byte          `gorm:"column:metadata;type:jsonb;default:'{}'" json:"-"`
	LastAccessedAt *time.Time      `gorm:"column:last_accessed_at" json:"last_accessed_at,omitempty"`
	ExpiresAt      *time.Time      `gorm:"column:expires_at" json:"expires_at,omitempty"`
	CreatedAt      time.Time       `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time       `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt      gorm.DeletedAt  `gorm:"column:deleted_at;index:idx_agent_memories_deleted" json:"-"`
}

// TableName overrides the GORM table name.
func (AgentMemory) TableName() string {
	return "agent_memories"
}

// MemoryRelation represents an edge between two memories.
type MemoryRelation struct {
	ID           string         `gorm:"column:id;type:varchar(36);primaryKey;default:uuid_generate_v4()" json:"id"`
	TenantID     string         `gorm:"column:tenant_id;type:varchar(36);not null;index" json:"tenant_id"`
	FromUUID     string         `gorm:"column:from_uuid;type:varchar(36);not null;index:idx_mr_from" json:"from_uuid"`
	ToUUID       string         `gorm:"column:to_uuid;type:varchar(36);not null;index:idx_mr_to" json:"to_uuid"`
	RelationType string         `gorm:"column:relation_type;type:varchar(64);default:''" json:"relation_type"`
	Weight       float64        `gorm:"column:weight;type:real;default:1.0" json:"weight"`
	CreatedAt    time.Time      `gorm:"column:created_at" json:"created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (MemoryRelation) TableName() string {
	return "memory_relations"
}

// DreamerState tracks the dreamer lock and run history of one (tenant,
// knowledge base) scope; kb_id ” is the tenant-wide row of older builds.
type DreamerState struct {
	ID          string     `gorm:"column:id;type:varchar(36);primaryKey;default:uuid_generate_v4()" json:"id"`
	TenantID    string     `gorm:"column:tenant_id;type:varchar(36);not null" json:"tenant_id"`
	KbID        string     `gorm:"column:kb_id;type:varchar(36);not null;default:''" json:"kb_id"`
	HeartbeatAt *time.Time `gorm:"column:heartbeat_at" json:"heartbeat_at,omitempty"`
	Runs        int        `gorm:"column:runs;default:0" json:"runs"`
	LastRunAt   *time.Time `gorm:"column:last_run_at" json:"last_run_at,omitempty"`
	LockedBy    string     `gorm:"column:locked_by;type:varchar(64);default:''" json:"locked_by"`
	LockedUntil *time.Time `gorm:"column:locked_until" json:"locked_until,omitempty"`
	Stats       []byte     `gorm:"column:stats;type:jsonb;default:'{}'" json:"stats,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (DreamerState) TableName() string {
	return "dreamer_state"
}

// ---------------------------------------------------------------------------
// Filter types
// ---------------------------------------------------------------------------

// MemoryScope is one (tenant, knowledge base) partition of the memory store.
type MemoryScope struct {
	TenantID string `json:"tenant_id"`
	KbID     string `json:"kb_id"`
}

// MemoryFilter represents search/filter parameters for memories.
type MemoryFilter struct {
	TenantID string `json:"tenant_id"`
	KbID     string `json:"kb_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	// AuthorUserID narrows results to rows written by one user. Unlike UserID
	// (carried for recall attribution and never applied as a SQL filter) it is
	// enforced by the repository; empty means every member's memories.
	AuthorUserID string          `json:"author_user_id,omitempty"`
	Query        string          `json:"query,omitempty"`
	MemoryType   string          `json:"memory_type,omitempty"`
	Tier         *int            `json:"tier,omitempty"` // nil means no filter; 0-3 are valid values
	SessionID    string          `json:"session_id,omitempty"`
	Verdicts     []MemoryVerdict `json:"verdicts,omitempty"`
	MinScore     float64         `json:"min_score,omitempty"`
	// EmbeddingModelID restricts vector search to rows embedded by this model
	// (plus legacy rows with no recorded model). Empty disables the filter.
	EmbeddingModelID string `json:"embedding_model_id,omitempty"`
	DeepGraph        bool   `json:"deep_graph,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Offset           int    `json:"offset,omitempty"`
}

// ---------------------------------------------------------------------------
// Search result
// ---------------------------------------------------------------------------

// MemorySearchResult wraps a memory with its relevance score.
type MemorySearchResult struct {
	Memory    *AgentMemory `json:"memory"`
	Score     float64      `json:"score"`
	StaleDays int          `json:"stale_days"`
	IsStale   bool         `json:"is_stale"`
}

// ---------------------------------------------------------------------------
// Lint / Save result
// ---------------------------------------------------------------------------
// Phase 0.5 — New domain types for Memory V2

// MemoryLintIssue describes a single lint finding.
type MemoryLintIssue struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"` // warning | error | info
	Message  string `json:"message"`
	SourceID string `json:"source_id,omitempty"`
}

// SaveMemoryResult is the outcome of saving a memory.
type SaveMemoryResult struct {
	Memory     *AgentMemory      `json:"memory"`
	Created    bool              `json:"created"`
	LintIssues []MemoryLintIssue `json:"lint_issues"`
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// MemoryHealthIssue describes a single health check finding.
type MemoryHealthIssue struct {
	Type        string `json:"type"`
	MemoryID    string `json:"memory_id"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // low | medium | high | critical
	Suggestion  string `json:"suggestion"`
}

// HealthReport is the overall health assessment.
type HealthReport struct {
	TenantID    string               `json:"tenant_id"`
	CheckedAt   time.Time            `json:"checked_at"`
	TotalIssues int                  `json:"total_issues"`
	BySeverity  map[string]int       `json:"by_severity"`
	Issues      []*MemoryHealthIssue `json:"issues"`
}

// ---------------------------------------------------------------------------
// Token budget
// ---------------------------------------------------------------------------

// TokenBudgetConfig controls token budget thresholds.
type TokenBudgetConfig struct {
	MaxTotalTokens    int `json:"max_total_tokens" yaml:"max_total_tokens"`     // default: 2000
	TruncateThreshold int `json:"truncate_threshold" yaml:"truncate_threshold"` // default: 1500
	SummaryThreshold  int `json:"summary_threshold" yaml:"summary_threshold"`   // default: 2500
	MaxPerMemory      int `json:"max_per_memory" yaml:"max_per_memory"`         // default: 300
}

// TokenBudgetInfo describes the applied budget mode.
type TokenBudgetInfo struct {
	Mode      string `json:"mode"` // full | truncated | summary
	Used      int    `json:"used"`
	Remaining int    `json:"remaining"`
}

// ---------------------------------------------------------------------------
// Dreamer
// ---------------------------------------------------------------------------

// DreamerConfig configures the dreamer worker.
type DreamerConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Interval    string `json:"interval" yaml:"interval"` // default: "6h"
	ModelID     string `json:"model_id" yaml:"model_id"`
	MaxActions  int    `json:"max_actions" yaml:"max_actions"`   // default: 5
	TokenBudget int    `json:"token_budget" yaml:"token_budget"` // max completion tokens per pass, default: 2000
	DryRun      bool   `json:"dry_run" yaml:"dry_run"`
	// BatchSize is how many memories one pass reviews (default 30).
	BatchSize int `json:"batch_size" yaml:"batch_size"`
	// MinConfidence drops proposals the model is less sure of (default 0.8).
	MinConfidence float64 `json:"min_confidence" yaml:"min_confidence"`
	// MergeLiveAfterRuns keeps merges as proposals for the first N passes of
	// a knowledge base, then applies them (default 3; 0 = never apply).
	MergeLiveAfterRuns int `json:"merge_live_after_runs" yaml:"merge_live_after_runs"`
	// MergeMinCosine is the server-verified similarity a merge needs (0.93).
	MergeMinCosine float64 `json:"merge_min_cosine" yaml:"merge_min_cosine"`
	// AllowRemove lets the dreamer soft-delete memories (default false).
	AllowRemove bool `json:"allow_remove" yaml:"allow_remove"`
	// DailyTokenBudget caps estimated tokens per tenant per day (0 = none).
	DailyTokenBudget int `json:"daily_token_budget" yaml:"daily_token_budget"`
	// MaxChangeRatio aborts a pass proposing changes to more than this share
	// of its batch (circuit breaker, default 0.34).
	MaxChangeRatio float64 `json:"max_change_ratio" yaml:"max_change_ratio"`
}

// DreamAction is a single action proposed by the dreamer.
type DreamAction struct {
	Type       string   `json:"type"`
	TargetID   string   `json:"target_id"`
	TargetIDs  []string `json:"target_ids,omitempty"`
	NewVerdict string   `json:"new_verdict,omitempty"`
	Delta      int      `json:"delta,omitempty"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
}

// DreamResult is the outcome of a dreamer pass.
type DreamResult struct {
	ActionsProposed int           `json:"actions_proposed"`
	ActionsApplied  int           `json:"actions_applied"`
	Actions         []DreamAction `json:"actions"`
	TokenUsed       int           `json:"token_used"`
}

// DreamStatus describes the dreamer of one knowledge base for the UI.
type DreamStatus struct {
	Enabled       bool                   `json:"enabled"`
	Scheduled     bool                   `json:"scheduled"` // background passes run (dreamer + worker kill switch on)
	DryRun        bool                   `json:"dry_run"`
	Interval      string                 `json:"interval"`
	Runs          int                    `json:"runs"`
	LastRunAt     *time.Time             `json:"last_run_at,omitempty"`
	Running       bool                   `json:"running"`
	MergeLive     bool                   `json:"merge_live"` // merge probation is over
	MergeLiveRuns int                    `json:"merge_live_after_runs"`
	LastStats     map[string]interface{} `json:"last_stats,omitempty"`
}

// ---------------------------------------------------------------------------
// Cache warmer
// ---------------------------------------------------------------------------

// CacheWarmerConfig is deprecated: the cache warmer was removed (it embedded
// memory contents and discarded the vectors). Kept so existing configs load.
type CacheWarmerConfig struct {
	Enabled         bool   `json:"enabled" yaml:"enabled"`
	TopQueriesN     int    `json:"top_queries_n" yaml:"top_queries_n"`       // default: 100
	RefreshInterval string `json:"refresh_interval" yaml:"refresh_interval"` // default: "30m"
}

// ---------------------------------------------------------------------------
// Lint-on-write
// ---------------------------------------------------------------------------

// LintOnWriteConfig configures lint checks run on every memory save.
type LintOnWriteConfig struct {
	Enabled                bool    `json:"enabled" yaml:"enabled"`
	StaleThresholdDays     int     `json:"stale_threshold_days" yaml:"stale_threshold_days"`         // default: 90
	ContradictionThreshold float64 `json:"contradiction_threshold" yaml:"contradiction_threshold"`   // default: 0.85
	NearDuplicateThreshold float64 `json:"near_duplicate_threshold" yaml:"near_duplicate_threshold"` // default: 0.90
}

// ---------------------------------------------------------------------------
// Recency boost
// ---------------------------------------------------------------------------

// RecencyBoostConfig configures the recency boost applied to search results.
type RecencyBoostConfig struct {
	Enabled             bool    `json:"enabled" yaml:"enabled"`
	ShortTermMultiplier float64 `json:"short_term_multiplier" yaml:"short_term_multiplier"` // default: 1.15
	ShortTermWindow     string  `json:"short_term_window" yaml:"short_term_window"`         // default: "1h"
	LongTermFactor      float64 `json:"long_term_factor" yaml:"long_term_factor"`           // default: 0.05
	LongTermHalfLife    int     `json:"long_term_half_life" yaml:"long_term_half_life"`     // default: 30 (days)
}

// ---------------------------------------------------------------------------
// Dedup config
// ---------------------------------------------------------------------------

// DedupConfig configures the deduplication rules.
type DedupConfig struct {
	ExactThreshold float64 `json:"exact_threshold" yaml:"exact_threshold"` // default: 0.97
	NearThreshold  float64 `json:"near_threshold" yaml:"near_threshold"`   // default: 0.93
	MaxMerges      int     `json:"max_merges" yaml:"max_merges"`           // default: 3
	MergeMaxChars  int     `json:"merge_max_chars" yaml:"merge_max_chars"` // default: 10000 (content max)
}

// ---------------------------------------------------------------------------
// MemoryV2Config
// ---------------------------------------------------------------------------

// MemoryV2Config is the top-level configuration for the Memory v2 module.
// See design.md section 2.7 for field specifications.
type MemoryV2Config struct {
	Enabled          bool               `json:"enabled" yaml:"enabled"`
	MaxSearchResults int                `json:"max_search_results" yaml:"max_search_results"`
	SemanticDedup    DedupConfig        `json:"semantic_dedup" yaml:"semantic_dedup"`
	RecencyBoost     RecencyBoostConfig `json:"recency_boost" yaml:"recency_boost"`
	TokenBudget      TokenBudgetConfig  `json:"token_budget" yaml:"token_budget"`
	Dreamer          DreamerConfig      `json:"dreamer" yaml:"dreamer"`
	CacheWarmer      CacheWarmerConfig  `json:"cache_warmer" yaml:"cache_warmer"`
	LintOnWrite      LintOnWriteConfig  `json:"lint_on_write" yaml:"lint_on_write"`
	// MinScoreThreshold is deprecated: fused scores are relative, see
	// MinCosine and RelativeScoreFloor. Kept so existing configs still load.
	MinScoreThreshold float64 `json:"min_score_threshold" yaml:"min_score_threshold"`
	// MinCosine drops vector-only matches below this cosine similarity
	// (keyword matches are kept). Calibrated on the recall eval.
	MinCosine float64 `json:"min_cosine" yaml:"min_cosine"`
	// RelativeScoreFloor drops results scoring below this fraction of the
	// best result's fused score.
	RelativeScoreFloor float64 `json:"relative_score_floor" yaml:"relative_score_floor"`
	HNSWM              int     `json:"hnsw_m" yaml:"hnsw_m"`                             // default: 16
	HNSWEfConstruction int     `json:"hnsw_ef_construction" yaml:"hnsw_ef_construction"` // default: 200
	HNSWEfSearch       int     `json:"hnsw_ef_search" yaml:"hnsw_ef_search"`             // default: 100
	// WorkerMutations is the kill switch for every background worker that
	// modifies or deletes stored memories (dreamer, consolidator decay,
	// pruner, entity extractor). Default true; each worker also has its own
	// flag. Env override: MEMORY_V2_WORKER_MUTATIONS=false stops them all.
	WorkerMutations bool `json:"worker_mutations" yaml:"worker_mutations"`
	// PrunerEnabled removes explicitly expired memories and never-read tier-3
	// memories after 30 days (default true).
	PrunerEnabled bool `json:"pruner_enabled" yaml:"pruner_enabled"`
	// ConsolidatorEnabled refreshes hub scores and decays the importance of
	// memories unused for a year (default true).
	ConsolidatorEnabled bool `json:"consolidator_enabled" yaml:"consolidator_enabled"`
	// EntityExtractorEnabled adds LLM-extracted entity tags (default false).
	EntityExtractorEnabled bool `json:"entity_extractor_enabled" yaml:"entity_extractor_enabled"`
}

// DefaultMemoryV2Config returns a config with sensible defaults.
func DefaultMemoryV2Config() MemoryV2Config {
	return MemoryV2Config{
		Enabled:          true,
		MaxSearchResults: 20,
		SemanticDedup: DedupConfig{
			ExactThreshold: 0.97,
			NearThreshold:  0.93,
			MaxMerges:      3,
			MergeMaxChars:  10000,
		},
		RecencyBoost: RecencyBoostConfig{
			Enabled:             true,
			ShortTermMultiplier: 1.15,
			ShortTermWindow:     "1h",
			LongTermFactor:      0.05,
			LongTermHalfLife:    30,
		},
		TokenBudget: TokenBudgetConfig{
			MaxTotalTokens:    2000,
			TruncateThreshold: 1500,
			SummaryThreshold:  2500,
			MaxPerMemory:      300,
		},
		Dreamer: DreamerConfig{
			Enabled:            true,
			Interval:           "6h",
			MaxActions:         5,
			TokenBudget:        2000,
			BatchSize:          30,
			MinConfidence:      0.8,
			MergeLiveAfterRuns: 3,
			MergeMinCosine:     0.93,
			DailyTokenBudget:   200000,
			MaxChangeRatio:     0.34,
		},
		CacheWarmer: CacheWarmerConfig{
			Enabled:         false,
			TopQueriesN:     100,
			RefreshInterval: "30m",
		},
		LintOnWrite: LintOnWriteConfig{
			Enabled:                true,
			StaleThresholdDays:     90,
			ContradictionThreshold: 0.85,
			NearDuplicateThreshold: 0.90,
		},
		MinScoreThreshold:   0.4,
		WorkerMutations:     true,
		PrunerEnabled:       true,
		ConsolidatorEnabled: true,
		MinCosine:           0.45,
		RelativeScoreFloor:  0.25,
		HNSWM:               16,
		HNSWEfConstruction:  200,
		HNSWEfSearch:        100,
	}
}

// Memory V2 readiness reasons. The ConcreteReason strings are reported to
// operators via GET /api/v1/tenants/memory-status and to logs, so they name
// the specific condition preventing Memory V2 from running.
const (
	// MemoryV2ReasonEnabled marks the module ready to serve requests.
	MemoryV2ReasonEnabled = "enabled"
	// MemoryV2ReasonConfigDisabled marks the module disabled via configuration.
	MemoryV2ReasonConfigDisabled = "disabled: memory_v2.enabled is false"
	// MemoryV2ReasonLiteMode marks the module disabled because Lite mode
	// (SQLite) cannot host the pgvector-backed memory tables.
	MemoryV2ReasonLiteMode = "disabled: Lite mode uses SQLite; Memory V2 requires PostgreSQL with pgvector"
	// MemoryV2ReasonRepoUnavailable marks the module not ready because its
	// repository is not available (nil).
	MemoryV2ReasonRepoUnavailable = "not_ready: repository unavailable"
)

// MemoryV2Readiness describes the module runtime state: whether requests and
// background workers may run, and the concrete reason when they may not.
type MemoryV2Readiness struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// MemoryStatusResponse is the typed response for GET /api/v1/tenants/memory-status.
type MemoryStatusResponse struct {
	Backend     string `json:"backend"`
	Available   bool   `json:"available"`
	MemoryCount int64  `json:"memory_count,omitempty"`
	Status      string `json:"status,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrMemoryValidation is returned when memory input fails validation; API
// layers map it to 400.
type ErrMemoryValidation struct {
	Message string
}

func (e *ErrMemoryValidation) Error() string {
	return "memory validation error: " + e.Message
}

// MemoryV2ConflictError is returned when a memory changed since the caller read
// it (optimistic lock) or a write would duplicate another memory; API layers
// map it to 409.
type MemoryV2ConflictError struct {
	Message    string
	ExistingID string
}

func (e *MemoryV2ConflictError) Error() string { return "memory conflict: " + e.Message }

// ErrProtectedVerdict is returned when an automated process tries to change a
// protected verdict (decision, fixed).
type ErrProtectedVerdict struct{}

func (e *ErrProtectedVerdict) Error() string {
	return "cannot change protected verdict (decision/fixed) via automated process"
}

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

// ActorKey is the context key for storing the actor identity ("dreamer", "system", "human").
type ActorKey struct{}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// NewMemory creates a new AgentMemory with sensible defaults.
func NewMemory(tenantID, content string) *AgentMemory {
	now := time.Now()
	return &AgentMemory{
		TenantID:    tenantID,
		Content:     content,
		MemoryType:  "semantic",
		Importance:  0,
		Tier:        2,
		Verdict:     VerdictNone,
		HubScore:    0,
		AccessCount: 0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// HubScoreFromDegree computes the hub score from in/out degree and average weight.
// Matches the SQL: LN(1 + degree) * avg_weight
func HubScoreFromDegree(degree, avgWeight float64) float64 {
	return math.Log(1+degree) * avgWeight
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

// Memory audit actions.
const (
	MemoryAuditCreated   = "created"
	MemoryAuditPatched   = "patched"
	MemoryAuditMerged    = "merged"
	MemoryAuditMergedIn  = "merged_into"
	MemoryAuditDeleted   = "deleted"
	MemoryAuditUnmerged  = "unmerged"
	MemoryAuditDreamed   = "dreamed"
	MemoryAuditReclaimed = "reclaimed"
)

// MemoryAuditEntry is one row of agent_memories_audit: who changed which
// memory, how, and its state before and after.
type MemoryAuditEntry struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TenantID  string    `gorm:"column:tenant_id" json:"tenant_id"`
	KbID      string    `gorm:"column:kb_id" json:"kb_id"`
	MemoryID  string    `gorm:"column:memory_id" json:"memory_id"`
	Actor     string    `gorm:"column:actor" json:"actor"`
	UserID    string    `gorm:"column:user_id" json:"user_id,omitempty"`
	Action    string    `gorm:"column:action" json:"action"`
	Before    []byte    `gorm:"column:before;type:jsonb" json:"-"`
	After     []byte    `gorm:"column:after;type:jsonb" json:"-"`
	Details   []byte    `gorm:"column:details;type:jsonb;default:'{}'" json:"-"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

// TableName overrides the GORM table name.
func (MemoryAuditEntry) TableName() string { return "agent_memories_audit" }

// MemoryAuditFilter selects audit entries of one knowledge base (and
// optionally one memory), newest first.
type MemoryAuditFilter struct {
	TenantID string
	KbID     string
	MemoryID string
	Actions  []string
	Limit    int
	Offset   int
}

// MemoryStats aggregates the live memories of a scope.
type MemoryStats struct {
	TotalMemories int64            `json:"total_memories"`
	ByType        map[string]int64 `json:"by_type"`
	ByTier        map[string]int64 `json:"by_tier"`
	ByVerdict     map[string]int64 `json:"by_verdict"`
	AvgImportance float64          `json:"avg_importance"`
	AvgHubScore   float64          `json:"avg_hub_score"`
}

// MemoryRelinkResult reports a knowledge-base relink pass.
type MemoryRelinkResult struct {
	Memories   int `json:"memories"`
	Relations  int `json:"relations"`
	Reembedded int `json:"reembedded"`
}
