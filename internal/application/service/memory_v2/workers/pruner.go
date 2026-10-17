package workers

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Pruning policy. Only memories that expired explicitly, or low-value edge
// memories nobody ever read, are removed; everything else is kept forever.
const (
	edgeTierMaxAge     = 30 * 24 * time.Hour // tier-3 memories unread this long are pruned
	hardDeleteAfter    = 30 * 24 * time.Hour // soft-deleted rows kept this long for undo
	pruneScanBatchSize = 1000
)

// PruneDeleter deletes through the service so every prune is audited.
type PruneDeleter interface {
	DeleteMemory(ctx context.Context, tenantID, id string) error
}

// Pruner removes memories that are explicitly expired or are unused tier-3
// ("edge") memories, and purges soft-deleted rows after a retention window.
// Never pruned: protected verdicts (decision/fixed), tiers 0-2, memories
// tagged critical/permanent, and anything that has ever been read.
type Pruner struct {
	repo    interfaces.MemoryRepositoryV2
	deleter PruneDeleter
	now     func() time.Time
}

// NewPruner creates a new Pruner.
func NewPruner(repo interfaces.MemoryRepositoryV2) *Pruner {
	return &Pruner{repo: repo, now: time.Now}
}

// WithDeleter routes prunes through the service (audited).
func (p *Pruner) WithDeleter(d PruneDeleter) *Pruner {
	p.deleter = d
	return p
}

// Run starts the pruner worker loop. Runs daily at 3:00 AM.
func (p *Pruner) Run(ctx context.Context) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		if now.After(next) {
			next = next.AddDate(0, 0, 1)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Sub(now)):
			p.pruneAll(ctx)
		}
	}
}

// pruneAll prunes every scope, then purges old soft-deleted rows per tenant.
func (p *Pruner) pruneAll(ctx context.Context) {
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")
	scopes, err := listScopes(ctx, p.repo)
	if err != nil {
		logger.Errorf(ctx, "pruner: list scopes failed: %v", err)
		return
	}
	tenants := map[string]bool{}
	pruned := 0
	for _, sc := range scopes {
		if ctx.Err() != nil {
			return
		}
		if sc.TenantID == "" {
			continue
		}
		tenants[sc.TenantID] = true
		pruned += p.softDeleteExpired(ctx, sc)
	}
	for tenantID := range tenants {
		p.hardDeleteSoftDeleted(ctx, tenantID)
	}
	logger.Infof(ctx, "pruner: cycle complete over %d scope(s), %d memories pruned", len(scopes), pruned)
}

// prunable reports whether policy allows removing m now.
func (p *Pruner) prunable(m *types.AgentMemory, now time.Time) bool {
	if m == nil || m.Verdict.IsProtected() || hasProtectedTag(m.Tags) {
		return false
	}
	if m.ExpiresAt != nil {
		// An explicit expiry date is authoritative either way.
		return m.ExpiresAt.Before(now)
	}
	return m.Tier >= 3 && m.AccessCount == 0 && m.Importance <= 0 &&
		now.Sub(m.CreatedAt) > edgeTierMaxAge && now.Sub(m.UpdatedAt) > edgeTierMaxAge
}

// softDeleteExpired prunes one scope; returns the number removed.
func (p *Pruner) softDeleteExpired(ctx context.Context, sc Scope) int {
	results, _, err := p.repo.Search(ctx, &types.MemoryFilter{TenantID: sc.TenantID, KbID: sc.KbID, Limit: pruneScanBatchSize})
	if err != nil {
		logger.Errorf(ctx, "pruner: search failed: %v", err)
		return 0
	}
	now := p.now()
	n := 0
	for _, r := range results {
		if !p.prunable(r.Memory, now) {
			continue
		}
		var err error
		if p.deleter != nil {
			err = p.deleter.DeleteMemory(ctx, sc.TenantID, r.Memory.ID)
		} else {
			err = p.repo.Delete(ctx, sc.TenantID, r.Memory.ID)
		}
		if err != nil {
			logger.Errorf(ctx, "pruner: soft-delete failed for %s: %v", r.Memory.ID, err)
			continue
		}
		n++
	}
	return n
}

// hardDeleteSoftDeleted purges soft-deleted, never-read memories of a tenant
// after the retention window.
func (p *Pruner) hardDeleteSoftDeleted(ctx context.Context, tenantID string) {
	count, err := p.repo.HardDeleteExpired(ctx, tenantID, p.now().Add(-hardDeleteAfter))
	if err != nil {
		logger.Errorf(ctx, "pruner: hard-delete failed: %v", err)
		return
	}
	if count > 0 {
		logger.Infof(ctx, "pruner: hard-deleted %d memories of tenant %s", count, tenantID)
	}
}

// hasProtectedTag checks if tags contain "critical" or "permanent".
func hasProtectedTag(tags []string) bool {
	for _, tag := range tags {
		if t := strings.ToLower(strings.TrimSpace(tag)); t == "critical" || t == "permanent" {
			return true
		}
	}
	return false
}
