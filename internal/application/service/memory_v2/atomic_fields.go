package memory_v2

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func (s *MemoryServiceV2Impl) writeAuditedFields(ctx context.Context, before *types.AgentMemory, fields map[string]interface{}, expected *time.Time, details map[string]interface{}) (*types.AgentMemory, error) {
	if expected == nil {
		version := before.UpdatedAt
		expected = &version
	}
	build := func(lockedBefore, after *types.AgentMemory) *types.MemoryAuditEntry {
		return newAudit(ctx, types.MemoryAuditPatched, lockedBefore, after, details)
	}
	if atomic, ok := s.repo.(interfaces.MemoryV2AtomicFieldWriter); ok {
		return atomic.UpdateFieldsAndAudit(ctx, before.TenantID, before.ID, fields, expected, build)
	}
	// Compatibility for in-memory test adapters. The production PostgreSQL
	// repository always takes the transaction above.
	updater := s.repo.(interfaces.MemoryV2FieldUpdater)
	if err := updater.UpdateFieldsIf(ctx, before.TenantID, before.ID, fields, expected); err != nil {
		return nil, err
	}
	after, err := s.repo.GetByID(ctx, before.TenantID, before.ID)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, build(before, after))
	return after, nil
}
