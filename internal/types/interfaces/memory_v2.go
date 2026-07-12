package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryRepositoryV2 defines the data-access contract for Memory v2.
type MemoryRepositoryV2 interface {
	// CRUD
	Create(ctx context.Context, memory *types.AgentMemory) error
	GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error)
	Update(ctx context.Context, memory *types.AgentMemory) error
	Delete(ctx context.Context, tenantID, id string) error

	// Search
	Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error)
	CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)

	// Dreamer gate
	TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error)
	UnlockDreamer(ctx context.Context, tenantID string) error

	// Hub score
	ComputeHubScores(ctx context.Context, tenantID string) error

	// Cache invalidation
	InvalidateResultCache(ctx context.Context, tenantID string)
}

// MemoryServiceV2 defines the business-logic contract for Memory v2.
type MemoryServiceV2 interface {
	// AddEpisode processes a conversation session and adds memories.
	AddEpisode(ctx context.Context, userID, sessionID string, messages []types.Message) error

	// RetrieveMemory retrieves relevant memory context.
	RetrieveMemory(ctx context.Context, userID, query string) (*types.MemoryContext, error)

	// SaveMemory saves a single memory through the full ingestion pipeline.
	SaveMemory(ctx context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error)

	// SearchMemories performs the hybrid search pipeline.
	SearchMemories(ctx context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error)

	// ConsolidateDream runs one dreamer pass.
	ConsolidateDream(ctx context.Context, tenantID string) (*types.DreamResult, error)

	// AssessHealth runs all 6 health checks.
	AssessHealth(ctx context.Context, tenantID string) ([]*types.MemoryHealthIssue, error)

	// StartWorkers launches background workers.
	StartWorkers(ctx context.Context)

	// Cleanup stops all workers gracefully.
	Cleanup()
}
