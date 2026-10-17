package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ interfaces.MemoryV2AtomicFieldWriter = (*MemoryRepository)(nil)
var _ interfaces.MemoryV2AtomicDeleter = (*MemoryRepository)(nil)

func lockedMemory(tx *gorm.DB, tenantID, id string) (*types.AgentMemory, error) {
	var m types.AgentMemory
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&m).Error
	return &m, err
}

func (r *MemoryRepository) UpdateFieldsAndAudit(ctx context.Context, tenantID, id string, fields map[string]interface{}, expected *time.Time, audit interfaces.MemoryV2AuditBuilder) (*types.AgentMemory, error) {
	var after *types.AgentMemory
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		before, err := lockedMemory(tx, tenantID, id)
		if err != nil {
			return err
		}
		scoped := NewMemoryRepository(tx)
		if err := scoped.UpdateFieldsIf(ctx, tenantID, id, fields, expected); err != nil {
			return err
		}
		after, err = scoped.GetByID(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if audit == nil {
			return fmt.Errorf("memory_v2: audit builder required")
		}
		entry := audit(before, after)
		if entry == nil {
			return fmt.Errorf("memory_v2: audit entry required")
		}
		return recordAudit(tx, []*types.MemoryAuditEntry{entry})
	})
	if err != nil {
		return nil, err
	}
	r.InvalidateResultCache(ctx, tenantID)
	return after, nil
}

func (r *MemoryRepository) DeleteAndAudit(ctx context.Context, tenantID, id string, audit interfaces.MemoryV2AuditBuilder) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		before, err := lockedMemory(tx, tenantID, id)
		if err != nil {
			return err
		}
		scoped := NewMemoryRepository(tx)
		if err := scoped.Delete(ctx, tenantID, id); err != nil {
			return err
		}
		if audit == nil {
			return fmt.Errorf("memory_v2: audit builder required")
		}
		entry := audit(before, nil)
		if entry == nil {
			return fmt.Errorf("memory_v2: audit entry required")
		}
		return recordAudit(tx, []*types.MemoryAuditEntry{entry})
	})
	if err == nil {
		r.InvalidateResultCache(ctx, tenantID)
	}
	return err
}
