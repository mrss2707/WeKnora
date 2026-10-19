package repository

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2AtomicCreator = (*MemoryRepository)(nil)

func (r *MemoryRepository) CreateAndAudit(ctx context.Context, memory *types.AgentMemory, audit interfaces.MemoryV2AuditBuilder) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewMemoryRepository(tx).Create(ctx, memory); err != nil {
			return err
		}
		if audit == nil {
			return fmt.Errorf("memory_v2: audit builder required")
		}
		entry := audit(nil, memory)
		if entry == nil {
			return fmt.Errorf("memory_v2: audit entry required")
		}
		if err := recordAudit(tx, []*types.MemoryAuditEntry{entry}); err != nil {
			return err
		}
		return enqueueGraphLink(tx, memory)
	})
}
