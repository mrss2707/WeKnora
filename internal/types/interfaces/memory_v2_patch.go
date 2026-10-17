package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2Patch is a partial update of one memory. A nil field is left
// unchanged, so an explicit zero (importance 0, tier 0) is distinct from
// "not provided". Content changes re-embed the memory. ExpectedUpdatedAt,
// when set, makes the write fail with types.MemoryV2ConflictError if the memory changed
// since the caller read it.
type MemoryV2Patch struct {
	Content           *string              `json:"content,omitempty"`
	ExpectedUpdatedAt *time.Time           `json:"expected_updated_at,omitempty"`
	Verdict           *types.MemoryVerdict `json:"verdict,omitempty"`
	Importance        *int                 `json:"importance,omitempty"`
	Tier              *int                 `json:"tier,omitempty"`
	MemoryType        *string              `json:"memory_type,omitempty"`
	Tags              *[]string            `json:"tags,omitempty"`
}

// IsEmpty reports whether the patch changes nothing.
func (p MemoryV2Patch) IsEmpty() bool {
	return p.Content == nil && p.Verdict == nil && p.Importance == nil && p.Tier == nil && p.MemoryType == nil && p.Tags == nil
}

// MemoryV2Patcher is an optional capability of MemoryServiceV2 (type-assert
// it) that applies a metadata patch without re-running ingestion.
type MemoryV2Patcher interface {
	PatchMemory(ctx context.Context, tenantID, id string, patch MemoryV2Patch) (*types.AgentMemory, error)
}

// MemoryV2FieldUpdater is an optional capability of MemoryRepositoryV2 that
// writes only the named columns of one memory, so omitted fields are never
// clobbered by a whole-row save.
type MemoryV2FieldUpdater interface {
	UpdateFields(ctx context.Context, tenantID, id string, fields map[string]interface{}) error
	// UpdateFieldsIf is UpdateFields guarded by an optimistic lock: when
	// expectedUpdatedAt is non-nil the row must still carry that updated_at,
	// otherwise *types.MemoryV2ConflictError is returned and nothing is written.
	UpdateFieldsIf(ctx context.Context, tenantID, id string, fields map[string]interface{}, expectedUpdatedAt *time.Time) error
}

// MemoryV2ScopedDreamer is an optional capability of MemoryServiceV2 that runs
// a dreamer pass restricted to one knowledge base of a tenant.
type MemoryV2ScopedDreamer interface {
	ConsolidateDreamScope(ctx context.Context, tenantID, kbID string) (*types.DreamResult, error)
}

// MemoryV2Deleter is an optional capability of MemoryServiceV2 that deletes a
// memory (and its relations) with an audit record.
type MemoryV2Deleter interface {
	DeleteMemory(ctx context.Context, tenantID, id string) error
}

// MemoryV2Undoer is an optional capability of MemoryServiceV2 that reverses
// the latest merge into a memory.
type MemoryV2Undoer interface {
	UndoMerge(ctx context.Context, tenantID, kbID, survivorID string) (*types.AgentMemory, error)
}

// MemoryV2HistoryReader is an optional capability of MemoryServiceV2 that
// lists the audit trail of a knowledge base or one memory.
type MemoryV2HistoryReader interface {
	History(ctx context.Context, filter types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error)
}

// MemoryV2StatsReader is an optional capability of MemoryRepositoryV2 that
// aggregates statistics for a scope in one query.
type MemoryV2StatsReader interface {
	Stats(ctx context.Context, tenantID, kbID string) (*types.MemoryStats, error)
}

// MemoryV2AccessTracker is an optional capability of MemoryRepositoryV2 that
// records memory reads (access_count, last_accessed_at) without touching
// updated_at.
type MemoryV2AccessTracker interface {
	TouchAccess(ctx context.Context, tenantID string, ids []string) error
}
