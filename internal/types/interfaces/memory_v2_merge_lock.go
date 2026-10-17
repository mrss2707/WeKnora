package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2VersionedMergeStore locks and checks all participants before a
// merge/undo, including changes made while the embedding model was running.
type MemoryV2VersionedMergeStore interface {
	MergeIntoVersioned(ctx context.Context, before, after *types.AgentMemory, losers []*types.AgentMemory, audit []*types.MemoryAuditEntry) error
	RestoreMergeVersioned(ctx context.Context, current, before *types.AgentMemory, losers []*types.AgentMemory, mergeDetails []byte, expectedAuditID int64, audit []*types.MemoryAuditEntry) error
}
