package workers

import (
	"context"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Consolidator periodically recomputes hub scores, decays importance of old
// memories, and merges near-duplicate memories, one (tenant, knowledge base)
// scope at a time. With a resolver each scope uses its own embedding model.
type Consolidator struct {
	repo     interfaces.MemoryRepositoryV2
	embedder embedding.Embedder
	resolver ModelResolver
	interval time.Duration
}

// WithResolver makes the consolidator resolve each scope's embedding model.
func (c *Consolidator) WithResolver(r ModelResolver) *Consolidator {
	c.resolver = r
	return c
}

// NewConsolidator creates a new Consolidator.
func NewConsolidator(repo interfaces.MemoryRepositoryV2, embedder embedding.Embedder) *Consolidator {
	return &Consolidator{
		repo:     repo,
		embedder: embedder,
		interval: 6 * time.Hour,
	}
}

// Run starts the consolidation worker loop.
func (c *Consolidator) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run once on startup
	c.consolidateAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.consolidateAll(ctx)
		}
	}
}

// consolidateAll runs the consolidation tasks for every scope that holds
// memories. Nothing runs when scopes cannot be enumerated: an unscoped pass
// would match no rows or act across tenants.
func (c *Consolidator) consolidateAll(ctx context.Context) {
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")
	scopes, err := listScopes(ctx, c.repo)
	if err != nil {
		logger.Errorf(ctx, "consolidator: list scopes failed: %v", err)
		return
	}
	logger.Infof(ctx, "consolidator: starting consolidation cycle over %d scope(s)", len(scopes))
	hubDone := map[string]bool{}
	for _, sc := range scopes {
		if ctx.Err() != nil {
			return
		}
		if sc.TenantID == "" {
			continue
		}
		// Step 1: Recompute hub scores (tenant-wide relation graph)
		if !hubDone[sc.TenantID] {
			hubDone[sc.TenantID] = true
			if err := c.repo.ComputeHubScores(ctx, sc.TenantID); err != nil {
				logger.Errorf(ctx, "consolidator: ComputeHubScores failed for tenant %s: %v", sc.TenantID, err)
			}
		}
		// Step 2: Decay importance of memories older than 1 year
		if err := c.decayOldMemories(ctx, sc); err != nil {
			logger.Errorf(ctx, "consolidator: decayOldMemories failed for %s/%s: %v", sc.TenantID, sc.KbID, err)
		}
		// Step 3: Merge near-duplicates (cosine > 0.93)
		if err := c.mergeNearDuplicates(ctx, sc); err != nil {
			logger.Errorf(ctx, "consolidator: mergeNearDuplicates failed for %s/%s: %v", sc.TenantID, sc.KbID, err)
		}
	}
	logger.Infof(ctx, "consolidator: consolidation cycle complete")
}

// embedderFor returns the embedder for a scope.
func (c *Consolidator) embedderFor(ctx context.Context, sc Scope) (embedding.Embedder, error) {
	if c.resolver != nil {
		emb, _, err := c.resolver.Embedder(ctx, sc.TenantID, sc.KbID)
		return emb, err
	}
	if c.embedder == nil {
		return nil, fmt.Errorf("consolidator: no embedder")
	}
	return c.embedder, nil
}

// decayOldMemories reduces importance by 10% for memories older than 1 year
// within one scope.
func (c *Consolidator) decayOldMemories(ctx context.Context, sc Scope) error {
	filter := &types.MemoryFilter{
		TenantID: sc.TenantID,
		KbID:     sc.KbID,
		Limit:    1000,
	}
	results, _, err := c.repo.Search(ctx, filter)
	if err != nil {
		return err
	}

	oneYearAgo := time.Now().AddDate(-1, 0, 0)
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		if r.Memory.CreatedAt.Before(oneYearAgo) && r.Memory.Importance > -5 {
			// Decay importance by 10% (round toward zero)
			decayed := int(math.Floor(float64(r.Memory.Importance) * 0.9))
			if decayed >= -5 {
				r.Memory.Importance = decayed
				r.Memory.UpdatedAt = time.Now()
				if err := c.repo.Update(ctx, r.Memory); err != nil {
					logger.Errorf(ctx, "consolidator: decay update failed for %s: %v", r.Memory.ID, err)
				}
			}
		}
	}

	return nil
}

// mergeNearDuplicates finds memory pairs with cosine similarity > 0.93 within
// one scope and merges them.
func (c *Consolidator) mergeNearDuplicates(ctx context.Context, sc Scope) error {
	filter := &types.MemoryFilter{
		TenantID: sc.TenantID,
		KbID:     sc.KbID,
		Limit:    1000,
	}
	results, _, err := c.repo.Search(ctx, filter)
	if err != nil {
		return err
	}
	embedder, err := c.embedderFor(ctx, sc)
	if err != nil {
		return err
	}

	// Group by tenant and knowledge base: never merge across either boundary,
	// even if the repository returned rows outside the requested scope.
	tenantGroups := make(map[string][]*types.AgentMemory)
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		key := r.Memory.TenantID + "\x00" + r.Memory.KbID
		tenantGroups[key] = append(tenantGroups[key], r.Memory)
	}

	for _, memories := range tenantGroups {
		if len(memories) < 2 {
			continue
		}

		// Embed each memory
		type memWithVec struct {
			mem *types.AgentMemory
			vec []float32
		}

		var mems []memWithVec
		for _, mem := range memories {
			vec, err := embedder.Embed(ctx, mem.Content)
			if err != nil {
				continue
			}
			mems = append(mems, memWithVec{mem: mem, vec: vec})
		}

		merged := make(map[string]bool)
		for i := 0; i < len(mems); i++ {
			if merged[mems[i].mem.ID] {
				continue
			}
			for j := i + 1; j < len(mems); j++ {
				if merged[mems[j].mem.ID] {
					continue
				}
				sim := cosineSimilarity(mems[i].vec, mems[j].vec)
				if sim > 0.93 {
					// Merge j into i
					mergedContent := mems[i].mem.Content + "\n---\n" + mems[j].mem.Content
					if utf8.RuneCountInString(mergedContent) > maxMergedContentChars {
						continue // never truncate: skip rather than lose content
					}
					mems[i].mem.Content = mergedContent
					if mems[j].mem.Importance > mems[i].mem.Importance {
						mems[i].mem.Importance = mems[j].mem.Importance
					}
					mems[i].mem.UpdatedAt = time.Now()

					if err := c.repo.Update(ctx, mems[i].mem); err != nil {
						logger.Errorf(ctx, "consolidator: merge update failed: %v", err)
						continue
					}
					// Soft-delete the merged-away memory
					if err := c.repo.Delete(ctx, mems[j].mem.TenantID, mems[j].mem.ID); err != nil {
						logger.Errorf(ctx, "consolidator: merge delete failed: %v", err)
					}
					merged[mems[j].mem.ID] = true
				}
			}
		}
	}

	return nil
}
