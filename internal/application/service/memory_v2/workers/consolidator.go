package workers

import (
	"context"
	"math"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Consolidator periodically recomputes hub scores, decays importance of old
// memories, and merges near-duplicate memories.
type Consolidator struct {
	repo      interfaces.MemoryRepositoryV2
	embedder  embedding.Embedder
	interval  time.Duration
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

// consolidateAll runs all consolidation tasks.
func (c *Consolidator) consolidateAll(ctx context.Context) {
	logger.Infof(ctx, "consolidator: starting consolidation cycle")

	// Step 1: Recompute hub scores
	if err := c.repo.ComputeHubScores(ctx, ""); err != nil {
		logger.Errorf(ctx, "consolidator: ComputeHubScores failed: %v", err)
	}

	// Step 2: Decay importance of memories older than 1 year
	if err := c.decayOldMemories(ctx); err != nil {
		logger.Errorf(ctx, "consolidator: decayOldMemories failed: %v", err)
	}

	// Step 3: Merge near-duplicates (cosine > 0.93)
	if err := c.mergeNearDuplicates(ctx); err != nil {
		logger.Errorf(ctx, "consolidator: mergeNearDuplicates failed: %v", err)
	}

	logger.Infof(ctx, "consolidator: consolidation cycle complete")
}

// decayOldMemories reduces importance by 10% for memories older than 1 year.
func (c *Consolidator) decayOldMemories(ctx context.Context) error {
	// Search for all memories (this is simplified; in production would use a
	// targeted DB query with age filter)
	filter := &types.MemoryFilter{
		Limit: 1000,
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

// mergeNearDuplicates finds memory pairs with cosine similarity > 0.93 and merges them.
func (c *Consolidator) mergeNearDuplicates(ctx context.Context) error {
	// Search for all memories in batches
	filter := &types.MemoryFilter{
		Limit: 1000,
	}
	results, _, err := c.repo.Search(ctx, filter)
	if err != nil {
		return err
	}

	// Group by tenant and check cosine similarity
	tenantGroups := make(map[string][]*types.AgentMemory)
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		tenantGroups[r.Memory.TenantID] = append(tenantGroups[r.Memory.TenantID], r.Memory)
	}

	for tenantID, memories := range tenantGroups {
		if len(memories) < 2 {
			continue
		}

		// Embed each memory
		type memWithVec struct {
			mem  *types.AgentMemory
			vec  []float32
		}

		var mems []memWithVec
		for _, mem := range memories {
			vec, err := c.embedder.Embed(ctx, mem.Content)
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
					if len(mergedContent) > 2000 {
						mergedContent = mergedContent[:2000]
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
					if err := c.repo.Delete(ctx, tenantID, mems[j].mem.ID); err != nil {
						logger.Errorf(ctx, "consolidator: merge delete failed: %v", err)
					}
					merged[mems[j].mem.ID] = true
				}
			}
		}
	}

	return nil
}
