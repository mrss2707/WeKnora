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
	RelatedEpisodes []Episode `json:"related_episodes"`
}

// ---------------------------------------------------------------------------
// GORM models
// ---------------------------------------------------------------------------

// AgentMemory represents a single memory record stored in agent_memories.
type AgentMemory struct {
	ID             string          `gorm:"column:id;type:varchar(36);primaryKey;default:uuid_generate_v4()" json:"id"`
	TenantID       string          `gorm:"column:tenant_id;type:varchar(36);not null;index:idx_agent_memories_tenant" json:"tenant_id"`
	KbID           string          `gorm:"column:kb_id;type:varchar(36);not null" json:"kb_id"`
	UserID         string          `gorm:"column:user_id;type:varchar(36);not null;default:''" json:"user_id"`
	Content        string          `gorm:"column:content;type:text;not null" json:"content"`
	MemoryType     string          `gorm:"column:memory_type;type:varchar(32);default:''" json:"memory_type"`
	Importance     int             `gorm:"column:importance;type:int;default:0" json:"importance"`
	Tier           int             `gorm:"column:tier;type:int;default:2" json:"tier"`
	Verdict        MemoryVerdict   `gorm:"column:verdict;type:varchar(16);default:none" json:"verdict"`
	HubScore       float64         `gorm:"column:hub_score;default:0" json:"hub_score"`
	Embedding      pgvector.Vector `gorm:"column:embedding;type:vector(1536)" json:"embedding"`
	AccessCount    int             `gorm:"column:access_count;type:int;default:0" json:"access_count"`
	SessionID      string          `gorm:"column:session_id;type:varchar(36);default:'';index:idx_agent_memories_session" json:"session_id"`
	Fingerprint    *string         `gorm:"column:fingerprint;type:varchar(64);index:idx_agent_memories_fingerprint,where:fingerprint IS NOT NULL AND deleted_at IS NULL" json:"fingerprint,omitempty"`
	Tags           TagsArray       `gorm:"column:tags;type:text[];default:'{}'" json:"tags,omitempty"`
	Metadata       []byte          `gorm:"column:metadata;type:jsonb;default:'{}'" json:"metadata,omitempty"`
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

// DreamerState tracks the dreamer worker lock for a tenant.
type DreamerState struct {
	ID          string     `gorm:"column:id;type:varchar(36);primaryKey;default:uuid_generate_v4()" json:"id"`
	TenantID    string     `gorm:"column:tenant_id;type:varchar(36);not null;uniqueIndex:idx_ds_tenant" json:"tenant_id"`
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

// MemoryFilter represents search/filter parameters for memories.
type MemoryFilter struct {
	TenantID   string          `json:"tenant_id"`
	KbID       string          `json:"kb_id,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
	Query      string          `json:"query,omitempty"`
	MemoryType string          `json:"memory_type,omitempty"`
	Tier       *int            `json:"tier,omitempty"` // nil means no filter; 0-3 are valid values
	SessionID  string          `json:"session_id,omitempty"`
	Verdicts   []MemoryVerdict `json:"verdicts,omitempty"`
	MinScore   float64         `json:"min_score,omitempty"`
	DeepGraph  bool            `json:"deep_graph,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
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
	MaxTotalTokens    int `json:"max_total_tokens"`     // default: 2000
	TruncateThreshold int `json:"truncate_threshold"`    // default: 1500
	SummaryThreshold  int `json:"summary_threshold"`     // default: 2500
	MaxPerMemory      int `json:"max_per_memory"`        // default: 300
}

// TokenBudgetInfo describes the applied budget mode.
type TokenBudgetInfo struct {
	Mode      string `json:"mode"`      // full | truncated | summary
	Used      int    `json:"used"`
	Remaining int    `json:"remaining"`
}

// ---------------------------------------------------------------------------
// Dreamer
// ---------------------------------------------------------------------------

// DreamerConfig configures the dreamer worker.
type DreamerConfig struct {
	Enabled     bool   `json:"enabled"`
	Interval    string `json:"interval"`     // default: "1h"
	ModelID     string `json:"model_id"`
	MaxActions  int    `json:"max_actions"`  // default: 5
	TokenBudget int    `json:"token_budget"` // default: 4000
	DryRun      bool   `json:"dry_run"`
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

// ---------------------------------------------------------------------------
// Cache warmer
// ---------------------------------------------------------------------------

// CacheWarmerConfig configures the cache warmer worker.
type CacheWarmerConfig struct {
	Enabled         bool   `json:"enabled"`
	TopQueriesN     int    `json:"top_queries_n"`     // default: 100
	RefreshInterval string `json:"refresh_interval"`  // default: "30m"
}

// ---------------------------------------------------------------------------
// Lint-on-write
// ---------------------------------------------------------------------------

// LintOnWriteConfig configures lint checks run on every memory save.
type LintOnWriteConfig struct {
	Enabled                bool    `json:"enabled"`
	StaleThresholdDays     int     `json:"stale_threshold_days"`     // default: 90
	ContradictionThreshold float64 `json:"contradiction_threshold"`  // default: 0.85
	NearDuplicateThreshold float64 `json:"near_duplicate_threshold"` // default: 0.90
}

// ---------------------------------------------------------------------------
// Recency boost
// ---------------------------------------------------------------------------

// RecencyBoostConfig configures the recency boost applied to search results.
type RecencyBoostConfig struct {
	Enabled             bool    `json:"enabled"`
	ShortTermMultiplier float64 `json:"short_term_multiplier"`  // default: 1.15
	ShortTermWindow     string  `json:"short_term_window"`      // default: "1h"
	LongTermFactor      float64 `json:"long_term_factor"`       // default: 0.05
	LongTermHalfLife    int     `json:"long_term_half_life"`    // default: 30 (days)
}

// ---------------------------------------------------------------------------
// Dedup config
// ---------------------------------------------------------------------------

// DedupConfig configures the deduplication rules.
type DedupConfig struct {
	ExactThreshold float64 `json:"exact_threshold"` // default: 0.97
	NearThreshold  float64 `json:"near_threshold"`  // default: 0.93
	MaxMerges      int     `json:"max_merges"`      // default: 3
	MergeMaxChars  int     `json:"merge_max_chars"` // default: 2000
}

// ---------------------------------------------------------------------------
// MemoryV2Config
// ---------------------------------------------------------------------------

// MemoryV2Config is the top-level configuration for the Memory v2 module.
// See design.md section 2.7 for field specifications.
type MemoryV2Config struct {
	Enabled             bool               `json:"enabled"`
	MaxSearchResults    int                `json:"max_search_results"`
	SemanticDedup       DedupConfig        `json:"semantic_dedup"`
	RecencyBoost        RecencyBoostConfig `json:"recency_boost"`
	TokenBudget         TokenBudgetConfig  `json:"token_budget"`
	Dreamer             DreamerConfig      `json:"dreamer"`
	CacheWarmer         CacheWarmerConfig  `json:"cache_warmer"`
	LintOnWrite         LintOnWriteConfig  `json:"lint_on_write"`
	MinScoreThreshold   float64            `json:"min_score_threshold"`    // default: 0.4
	HNSWM               int                `json:"hnsw_m"`               // default: 16
	HNSWEfConstruction  int                `json:"hnsw_ef_construction"` // default: 200
	HNSWEfSearch        int                `json:"hnsw_ef_search"`       // default: 100
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
			MergeMaxChars:  2000,
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
			Enabled:     true,
			Interval:    "1h",
			MaxActions:  5,
			TokenBudget: 4000,
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
		MinScoreThreshold:  0.4,
		HNSWM:              16,
		HNSWEfConstruction: 200,
		HNSWEfSearch:       100,
	}
}

// MemoryStatusResponse is the typed response for GET /api/v1/tenants/memory-status.
type MemoryStatusResponse struct {
	Backend     string `json:"backend"`
	Available   bool   `json:"available"`
	MemoryCount int64  `json:"memory_count,omitempty"`
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

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
