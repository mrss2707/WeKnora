package repository

import (
	"context"
	"math"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2GraphStore = (*MemoryRepository)(nil)

// GetByIDs loads live memories of one tenant (embedding excluded).
func (r *MemoryRepository) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]*types.AgentMemory, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []*types.AgentMemory
	err := r.db.WithContext(ctx).Table("agent_memories").Select(memoryColumns).
		Where("tenant_id = ? AND id IN ? AND deleted_at IS NULL", tenantID, ids).
		Find(&out).Error
	return out, err
}

// RelationsOf returns live relations touching any of the ids whose other
// endpoint is a live memory, strongest first.
func (r *MemoryRepository) RelationsOf(ctx context.Context, tenantID string, ids []string, limit int) ([]*types.MemoryRelation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Table("memory_relations AS r").Select("r.*").
		Joins("JOIN agent_memories f ON f.id = r.from_uuid AND f.deleted_at IS NULL").
		Joins("JOIN agent_memories t ON t.id = r.to_uuid AND t.deleted_at IS NULL").
		Where("r.tenant_id = ? AND r.deleted_at IS NULL", tenantID).
		Where("(r.from_uuid IN ? OR r.to_uuid IN ?)", ids, ids).
		Order("r.weight DESC, r.id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []*types.MemoryRelation
	return out, q.Scan(&out).Error
}

// TopHubs returns the most connected live memories of a scope (highest hub
// score, then newest), for a knowledge-base graph overview.
func (r *MemoryRepository) TopHubs(ctx context.Context, tenantID, kbID string, limit int) ([]*types.AgentMemory, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Table("agent_memories").Select(memoryColumns).
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if kbID != "" {
		q = q.Where("kb_id = ?", kbID)
	}
	var out []*types.AgentMemory
	return out, q.Order("hub_score DESC, created_at DESC, id").Limit(limit).Find(&out).Error
}

var _ interfaces.MemoryV2EmbeddingReader = (*MemoryRepository)(nil)

// ListWithEmbeddings pages through a knowledge base's live memories,
// including vectors and metadata, oldest first.
func (r *MemoryRepository) ListWithEmbeddings(ctx context.Context, tenantID, kbID string, limit, offset int) ([]*types.AgentMemory, error) {
	var out []*types.AgentMemory
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND kb_id = ? AND deleted_at IS NULL", tenantID, kbID).
		Order("created_at, id").Limit(limit).Offset(offset).Find(&out).Error
	return out, err
}

var _ interfaces.MemoryV2PairSimilarity = (*MemoryRepository)(nil)

// PairSimilarity returns the cosine similarity of two live memories' stored
// vectors (0 when either is missing).
func (r *MemoryRepository) PairSimilarity(ctx context.Context, tenantID, a, b string) (float64, error) {
	var sim *float64
	err := r.db.WithContext(ctx).Raw(`SELECT 1 - (x.embedding <=> y.embedding)
FROM agent_memories x, agent_memories y
WHERE x.tenant_id = ? AND y.tenant_id = ? AND x.id = ? AND y.id = ?
  AND x.deleted_at IS NULL AND y.deleted_at IS NULL
  AND x.embedding IS NOT NULL AND y.embedding IS NOT NULL`, tenantID, tenantID, a, b).Scan(&sim).Error
	if err != nil || sim == nil || math.IsNaN(*sim) {
		return 0, err
	}
	return *sim, nil
}
