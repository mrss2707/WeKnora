package workers

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// CacheWarmer periodically warms the embedding cache with frequent queries.
type CacheWarmer struct {
	repo       interfaces.MemoryRepositoryV2
	embedder   embedding.Embedder
	config     types.CacheWarmerConfig
	interval   time.Duration
}

// NewCacheWarmer creates a new CacheWarmer.
func NewCacheWarmer(repo interfaces.MemoryRepositoryV2, embedder embedding.Embedder, config types.CacheWarmerConfig) *CacheWarmer {
	return &CacheWarmer{
		repo:     repo,
		embedder: embedder,
		config:   config,
		interval: parseDuration(config.RefreshInterval, 30*time.Minute),
	}
}

// Run starts the cache warmer worker loop.
func (cw *CacheWarmer) Run(ctx context.Context) {
	if !cw.config.Enabled {
		logger.Infof(ctx, "cache-warmer: disabled, not starting")
		return
	}

	logger.Infof(ctx, "cache-warmer: starting with interval %s", cw.interval)

	// Warm on startup
	cw.warmCache(ctx)

	ticker := time.NewTicker(cw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cw.warmCache(ctx)
		}
	}
}

// warmCache pre-computes embeddings for top frequent queries.
func (cw *CacheWarmer) warmCache(ctx context.Context) {
	logger.Infof(ctx, "cache-warmer: warming cache")

	// Fetch recent memories to pre-warm embedding cache
	topN := cw.config.TopQueriesN
	if topN <= 0 {
		topN = 100
	}

	filter := &types.MemoryFilter{
		Limit: topN,
	}
	results, _, err := cw.repo.Search(ctx, filter)
	if err != nil {
		logger.Errorf(ctx, "cache-warmer: search failed: %v", err)
		return
	}

	for _, r := range results {
		if r.Memory == nil || r.Memory.Content == "" {
			continue
		}

		// Pre-compute and cache embedding by embedding the content
		_, err := cw.embedder.Embed(ctx, r.Memory.Content)
		if err != nil {
			logger.Errorf(ctx, "cache-warmer: embedding failed for memory %s: %v", r.Memory.ID, err)
			continue
		}
	}

	logger.Infof(ctx, "cache-warmer: warmed %d embeddings", len(results))
}
