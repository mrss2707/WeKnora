package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2Auditor is an optional capability of MemoryRepositoryV2 that
// records and lists the per-memory audit trail.
type MemoryV2Auditor interface {
	RecordAudit(ctx context.Context, entries ...*types.MemoryAuditEntry) error
	ListAudit(ctx context.Context, filter types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error)
}

// MemoryV2MergeStore is an optional capability of MemoryRepositoryV2 that
// performs a merge atomically: the survivor is written, every loser is
// soft-deleted, the losers' relations are re-pointed to the survivor (without
// duplicates or self-loops) and the audit rows are stored, all in one
// transaction.
type MemoryV2MergeStore interface {
	MergeInto(ctx context.Context, survivor *types.AgentMemory, loserIDs []string, audit []*types.MemoryAuditEntry) error
	// RestoreMerge reverses MergeInto from the audit before-images: the
	// survivor's fields and the losers are restored in one transaction.
	RestoreMerge(ctx context.Context, survivorBefore *types.AgentMemory, losers []*types.AgentMemory, audit []*types.MemoryAuditEntry) error
}
