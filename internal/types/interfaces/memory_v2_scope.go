package interfaces

import (
	"context"

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
