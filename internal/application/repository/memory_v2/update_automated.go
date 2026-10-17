package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Unlike Save's upsert fallback, a worker update must neither resurrect a
// deleted row nor overwrite a verdict protected after its earlier read.
func (r *MemoryRepository) updateAutomated(ctx context.Context, memory *types.AgentMemory) error {
	result := r.db.WithContext(ctx).Model(&types.AgentMemory{}).
		Where("tenant_id = ? AND id = ? AND verdict NOT IN ?", memory.TenantID, memory.ID, []types.MemoryVerdict{types.VerdictDecision, types.VerdictFixed}).
		Select("*").Updates(memory)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		if current, err := r.GetByID(ctx, memory.TenantID, memory.ID); err == nil && current.Verdict.IsProtected() {
			return &types.ErrProtectedVerdict{}
		}
		return gorm.ErrRecordNotFound
	}
	return nil
}
