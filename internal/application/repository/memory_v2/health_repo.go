package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2HealthEmbeddingReader = (*MemoryRepository)(nil)

// GetHealthEmbeddingDimension samples only live rows in the requested scope.
// Provenance/semantic compatibility is not inferred from dimension alone.
func (r *MemoryRepository) GetHealthEmbeddingDimension(ctx context.Context, tenantID, kbID string) (int, error) {
	q := r.db.WithContext(ctx).Table("agent_memories").
		Select("vector_dims(embedding)").
		Where("tenant_id = ? AND deleted_at IS NULL AND embedding IS NOT NULL", tenantID)
	if kbID != "" {
		q = q.Where("kb_id = ?", kbID)
	}
	var dimension int
	err := q.Order("id").Limit(1).Scan(&dimension).Error
	return dimension, err
}
