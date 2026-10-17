package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2ScopeLister = (*MemoryRepository)(nil)

// ListMemoryScopes returns every (tenant, knowledge base) pair that holds at
// least one live memory, in a stable order.
func (r *MemoryRepository) ListMemoryScopes(ctx context.Context) ([]types.MemoryScope, error) {
	var scopes []types.MemoryScope
	err := r.db.WithContext(ctx).Table("agent_memories").
		Select("DISTINCT tenant_id, kb_id").
		Where("deleted_at IS NULL").
		Order("tenant_id, kb_id").
		Scan(&scopes).Error
	return scopes, err
}
