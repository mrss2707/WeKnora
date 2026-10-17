package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2FieldUpdater = (*MemoryRepository)(nil)

// UpdateFields writes only the given columns of one live memory. Automated
// actors cannot change a memory whose verdict is protected.
func (r *MemoryRepository) UpdateFields(ctx context.Context, tenantID, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	if isAutomatedActor(ctx) {
		existing, err := r.GetByID(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if existing.Verdict.IsProtected() {
			return &types.ErrProtectedVerdict{}
		}
	}
	updates := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		updates[k] = v
	}
	updates["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&types.AgentMemory{}).
		Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, id).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	r.InvalidateResultCache(ctx, tenantID)
	return nil
}
