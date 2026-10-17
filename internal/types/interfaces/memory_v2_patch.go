package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2Patch is a partial metadata update of one memory. A nil field is
// left unchanged, so an explicit zero (importance 0, tier 0) is distinct from
// "not provided".
type MemoryV2Patch struct {
	Verdict    *types.MemoryVerdict `json:"verdict,omitempty"`
	Importance *int                 `json:"importance,omitempty"`
	Tier       *int                 `json:"tier,omitempty"`
	MemoryType *string              `json:"memory_type,omitempty"`
	Tags       *[]string            `json:"tags,omitempty"`
}

// IsEmpty reports whether the patch changes nothing.
func (p MemoryV2Patch) IsEmpty() bool {
	return p.Verdict == nil && p.Importance == nil && p.Tier == nil && p.MemoryType == nil && p.Tags == nil
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
}
