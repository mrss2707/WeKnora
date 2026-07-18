package workers

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Pruner removes expired memories based on tier policies.
type Pruner struct {
	repo interfaces.MemoryRepositoryV2
}

// NewPruner creates a new Pruner.
func NewPruner(repo interfaces.MemoryRepositoryV2) *Pruner {
	return &Pruner{repo: repo}
}

// Run starts the pruner worker loop. Runs daily at 3:00 AM.
func (p *Pruner) Run(ctx context.Context) {
	for {
		// Calculate time until next 3:00 AM
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		if now.After(next) {
			next = next.AddDate(0, 0, 1)
		}
		delay := next.Sub(now)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			p.pruneAll(ctx)

			// Wait 24h for next run
			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}
}

// pruneAll runs all pruning operations.
func (p *Pruner) pruneAll(ctx context.Context) {
	logger.Infof(ctx, "pruner: starting pruning cycle")

	// Step 1: Soft-delete expired memories
	p.softDeleteExpired(ctx)

	// Step 2: Hard-delete eligible soft-deleted memories
	p.hardDeleteSoftDeleted(ctx)

	logger.Infof(ctx, "pruner: pruning cycle complete")
}

// softDeleteExpired soft-deletes memories that have expired based on their tier.
// - Tier-3 (Edge): Delete immediately past TTL (7 days)
// - Tier-2 (Standard): Delete if expired AND not accessed >30 days
// - Tier-1 (Core): Delete if expired AND not accessed >30 days
// - Tier-0 (Critical): NEVER delete
// - Tagged "critical" or "permanent": NEVER delete
func (p *Pruner) softDeleteExpired(ctx context.Context) {
	filter := &types.MemoryFilter{
		Limit: 1000,
	}
	results, _, err := p.repo.Search(ctx, filter)
	if err != nil {
		logger.Errorf(ctx, "pruner: search failed: %v", err)
		return
	}

	now := time.Now()
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		mem := r.Memory

		// NEVER delete tier-0
		if mem.Tier == 0 {
			continue
		}

		// NEVER delete critical/permanent tagged memories
		if hasProtectedTag(mem.Tags) {
			continue
		}

		var shouldDelete bool

		switch mem.Tier {
		case 3: // Edge: 7 days TTL
			if now.After(mem.CreatedAt.Add(7 * 24 * time.Hour)) {
				shouldDelete = true
			}
		case 2: // Standard: 30 days TTL + 30 days inactivity
			ttlExpired := now.After(mem.CreatedAt.Add(30 * 24 * time.Hour))
			inactive := int(now.Sub(mem.UpdatedAt).Hours()/24) > 30
			if ttlExpired && inactive {
				shouldDelete = true
			}
		case 1: // Core: 90 days TTL + 30 days inactivity
			ttlExpired := now.After(mem.CreatedAt.Add(90 * 24 * time.Hour))
			inactive := int(now.Sub(mem.UpdatedAt).Hours()/24) > 30
			if ttlExpired && inactive {
				shouldDelete = true
			}
		}

		if shouldDelete {
			if err := p.repo.Delete(ctx, mem.TenantID, mem.ID); err != nil {
				logger.Errorf(ctx, "pruner: soft-delete failed for %s: %v", mem.ID, err)
			}
		}
	}
}

// hardDeleteSoftDeleted permanently deletes tier-3 soft-deleted memories
// that have been deleted for >14 days with access_count=0.
func (p *Pruner) hardDeleteSoftDeleted(ctx context.Context) {
	count, err := p.repo.HardDeleteExpired(ctx, "", time.Now().Add(-14*24*time.Hour))
	if err != nil {
		logger.Errorf(ctx, "pruner: hard-delete failed: %v", err)
		return
	}
	if count > 0 {
		logger.Infof(ctx, "pruner: hard-deleted %d expired memories", count)
	}
}

// hasProtectedTag checks if tags contain "critical" or "permanent".
func hasProtectedTag(tags []string) bool {
	for _, tag := range tags {
		lower := toLower(tag)
		if lower == "critical" || lower == "permanent" {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}
