package workers

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Decay policy: a memory nobody has read or changed for a year slowly loses
// importance, never below decayFloor, one step per consolidation cycle.
const (
	decayAfter = 365 * 24 * time.Hour
	decayFloor = 0
)

// Consolidator keeps derived signals fresh, one (tenant, knowledge base)
// scope at a time: it recomputes hub scores and gently decays the importance
// of long-unused memories. It never merges or deletes (the dreamer merges,
// through the audited merge transaction).
type Consolidator struct {
	repo     interfaces.MemoryRepositoryV2
	patcher  interfaces.MemoryV2Patcher
	interval time.Duration
	now      func() time.Time
}

// NewConsolidator creates a new Consolidator. The embedder is unused and kept
// for compatibility.
func NewConsolidator(repo interfaces.MemoryRepositoryV2, _ embedding.Embedder) *Consolidator {
	return &Consolidator{repo: repo, interval: 6 * time.Hour, now: time.Now}
}

// WithResolver is kept for wiring compatibility; the consolidator needs no model.
func (c *Consolidator) WithResolver(ModelResolver) *Consolidator { return c }

// WithPatcher routes importance decay through the audited service path.
func (c *Consolidator) WithPatcher(p interfaces.MemoryV2Patcher) *Consolidator {
	c.patcher = p
	return c
}

// Run starts the consolidation worker loop.
func (c *Consolidator) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
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

// consolidateAll processes every scope that holds memories. Nothing runs when
// scopes cannot be enumerated.
func (c *Consolidator) consolidateAll(ctx context.Context) {
	started, outcome := time.Now(), "completed"
	defer func() { telemetry.WorkerRun("consolidator", outcome, started) }()
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")
	scopes, err := listScopes(ctx, c.repo)
	if err != nil {
		outcome = "failed"
		logger.Errorf(ctx, "consolidator: list scopes failed: %v", err)
		return
	}
	hubDone := map[string]bool{}
	decayed := 0
	for _, sc := range scopes {
		if ctx.Err() != nil {
			outcome = "cancelled"
			return
		}
		if sc.TenantID == "" {
			continue
		}
		if !hubDone[sc.TenantID] {
			hubDone[sc.TenantID] = true
			if err := c.repo.ComputeHubScores(ctx, sc.TenantID); err != nil {
				outcome = "failed"
				logger.Errorf(ctx, "consolidator: hub scores for tenant %s: %v", sc.TenantID, err)
			}
		}
		n, err := c.decayOldMemories(ctx, sc)
		if err != nil {
			outcome = "failed"
			logger.Errorf(ctx, "consolidator: decay %s/%s: %v", sc.TenantID, sc.KbID, err)
		}
		decayed += n
	}
	telemetry.WorkerActions("consolidator", "applied", decayed)
	logger.Infof(ctx, "consolidator: cycle complete over %d scope(s), %d memories decayed", len(scopes), decayed)
}

// decayOldMemories lowers by one the importance of memories in the scope that
// were neither read nor changed for a year. Protected memories never decay.
func (c *Consolidator) decayOldMemories(ctx context.Context, sc Scope) (int, error) {
	updater, ok := c.repo.(interfaces.MemoryV2FieldUpdater)
	if !ok {
		return 0, nil
	}
	results, _, err := c.repo.Search(ctx, &types.MemoryFilter{TenantID: sc.TenantID, KbID: sc.KbID, Limit: 1000})
	if err != nil {
		return 0, err
	}
	cutoff := c.now().Add(-decayAfter)
	n := 0
	for _, r := range results {
		m := r.Memory
		if m == nil || m.Verdict.IsProtected() || m.Importance <= decayFloor {
			continue
		}
		last := m.UpdatedAt
		if m.LastAccessedAt != nil && m.LastAccessedAt.After(last) {
			last = *m.LastAccessedAt
		}
		if last.After(cutoff) {
			continue
		}
		version := m.UpdatedAt
		importance := m.Importance - 1
		var err error
		if c.patcher != nil {
			_, err = c.patcher.PatchMemory(ctx, sc.TenantID, m.ID, interfaces.MemoryV2Patch{Importance: &importance, ExpectedUpdatedAt: &version})
		} else {
			err = updater.UpdateFieldsIf(ctx, sc.TenantID, m.ID, map[string]interface{}{"importance": importance}, &version)
		}
		if err != nil {
			logger.Infof(ctx, "consolidator: skip decay of %s: %v", m.ID, err)
			continue
		}
		n++
	}
	return n, nil
}
