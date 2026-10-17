package memory_v2

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const orphanReclaimBatch = 500

// ReclaimOrphanedMemories soft-deletes (with relations and an audit row) the
// memories whose knowledge base was deleted. Deleting a knowledge base does
// not reach into this module, so the cleanup runs here. Audit mode only
// counts them. Returns the number of orphans found.
func ReclaimOrphanedMemories(ctx context.Context, repo interfaces.MemoryRepositoryV2, mode KBScopeRepairMode) (int, error) {
	if mode == KBScopeRepairOff {
		return 0, nil
	}
	reclaimer, ok := repo.(interfaces.MemoryV2OrphanReclaimer)
	if !ok {
		return 0, nil
	}
	auditor, _ := repo.(interfaces.MemoryV2Auditor)
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")
	found := 0
	for {
		batch, err := reclaimer.ListOrphanedKBMemories(ctx, orphanReclaimBatch)
		if err != nil {
			return found, err
		}
		found += len(batch)
		if len(batch) == 0 || mode == KBScopeRepairAudit {
			return found, nil
		}
		byTenant := map[string][]*types.AgentMemory{}
		for _, m := range batch {
			byTenant[m.TenantID] = append(byTenant[m.TenantID], m)
		}
		for tenantID, mems := range byTenant {
			ids := make([]string, 0, len(mems))
			audit := make([]*types.MemoryAuditEntry, 0, len(mems))
			for _, m := range mems {
				ids = append(ids, m.ID)
				audit = append(audit, newAudit(ctx, types.MemoryAuditReclaimed, m, nil, map[string]interface{}{"reason": "knowledge base deleted"}))
			}
			if err := reclaimer.SoftDeleteMemoriesWithRelations(ctx, tenantID, ids); err != nil {
				return found, err
			}
			if auditor != nil {
				if err := auditor.RecordAudit(ctx, audit...); err != nil {
					logger.Warnf(ctx, "[MemoryV2] orphan reclaim audit failed: %v", err)
				}
			}
		}
		if len(batch) < orphanReclaimBatch {
			return found, nil
		}
	}
}
