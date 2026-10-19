package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2VersionedRelationWriter rejects stale graph candidates and edges to
// deleted or differently scoped endpoints, with locks held through insertion.
type MemoryV2VersionedRelationWriter interface {
	CreateRelationForVersions(ctx context.Context, relation *types.MemoryRelation, fromVersion, toVersion time.Time) (bool, error)
}

// MemoryV2GraphJobs is optional; PostgreSQL supplies durable queueing, lease
// takeover and generation-checked completion without changing core interfaces.
type MemoryV2GraphJobs interface {
	EnqueueGraphLink(ctx context.Context, memory *types.AgentMemory) error
	ClaimGraphLink(ctx context.Context, workerID string, ttl time.Duration) (*types.MemoryGraphJob, error)
	FinishGraphLink(ctx context.Context, job *types.MemoryGraphJob, success bool) error
}
