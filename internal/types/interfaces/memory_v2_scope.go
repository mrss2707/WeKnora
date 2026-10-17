package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2ScopeLister is an optional capability of MemoryRepositoryV2 that
// enumerates the (tenant, knowledge base) scopes holding live memories, so
// background workers can process one scope at a time.
type MemoryV2ScopeLister interface {
	ListMemoryScopes(ctx context.Context) ([]types.MemoryScope, error)
}

// MemoryV2KeywordRanker is an optional capability of MemoryRepositoryV2 that
// ranks memories by keyword relevance (BM25) instead of recency.
type MemoryV2KeywordRanker interface {
	BM25Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error)
}

// MemoryV2HubRefresher is an optional capability of MemoryRepositoryV2 that
// recomputes the hub scores of specific memories.
type MemoryV2HubRefresher interface {
	RefreshHubScores(ctx context.Context, tenantID string, ids []string) error
}

// MemoryV2GraphStore is an optional capability of MemoryRepositoryV2 that
// loads graph neighbourhoods in batches (one query per BFS layer).
type MemoryV2GraphStore interface {
	GetByIDs(ctx context.Context, tenantID string, ids []string) ([]*types.AgentMemory, error)
	RelationsOf(ctx context.Context, tenantID string, ids []string, limit int) ([]*types.MemoryRelation, error)
	TopHubs(ctx context.Context, tenantID, kbID string, limit int) ([]*types.AgentMemory, error)
}

// MemoryV2EmbeddingReader is an optional capability of MemoryRepositoryV2
// that pages through a knowledge base's memories including their vectors.
type MemoryV2EmbeddingReader interface {
	ListWithEmbeddings(ctx context.Context, tenantID, kbID string, limit, offset int) ([]*types.AgentMemory, error)
}

// MemoryV2Relinker is an optional capability of MemoryServiceV2 that rebuilds
// a knowledge base's automatic relations.
type MemoryV2Relinker interface {
	RelinkKnowledgeBase(ctx context.Context, tenantID, kbID string) (*types.MemoryRelinkResult, error)
}

// MemoryV2ScopeLock is an optional capability of MemoryRepositoryV2 that
// coordinates the dreamer per (tenant, knowledge base) across replicas.
type MemoryV2ScopeLock interface {
	TryScopeLock(ctx context.Context, tenantID, kbID, workerID string, ttl time.Duration) (bool, *types.DreamerState, error)
	HeartbeatScopeLock(ctx context.Context, tenantID, kbID, workerID string, ttl time.Duration) (bool, error)
	ReleaseScopeLock(ctx context.Context, tenantID, kbID, workerID string, ran bool, stats map[string]interface{}) error
	ScopeChangedSince(ctx context.Context, tenantID, kbID string, t *time.Time) (bool, error)
	LeastRecentlyDreamed(ctx context.Context, tenantID, kbID string, limit int) ([]*types.AgentMemory, error)
	MarkDreamed(ctx context.Context, tenantID string, ids []string, at time.Time) error
}

// MemoryV2PairSimilarity is an optional capability of MemoryRepositoryV2 that
// compares two memories' stored vectors (cosine similarity).
type MemoryV2PairSimilarity interface {
	PairSimilarity(ctx context.Context, tenantID, a, b string) (float64, error)
}

// MemoryV2DreamStateReader is an optional capability of MemoryRepositoryV2
// that reads the dreamer state of one (tenant, knowledge base) scope.
type MemoryV2DreamStateReader interface {
	DreamState(ctx context.Context, tenantID, kbID string) (*types.DreamerState, error)
}
