package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// compile-time interface check
var _ interfaces.MemoryRepositoryV2 = (*MemoryRepository)(nil)

// MemoryRepository implements MemoryRepositoryV2 using GORM + PostgreSQL/pgvector.
type MemoryRepository struct {
	db *gorm.DB
}

// NewMemoryRepository creates a new MemoryRepository.
func NewMemoryRepository(db *gorm.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// Create inserts a new memory.
func (r *MemoryRepository) Create(ctx context.Context, memory *types.AgentMemory) error {
	if memory.ID == "" {
		memory.ID = uuid.New().String()
	}
	now := time.Now()
	memory.CreatedAt = now
	memory.UpdatedAt = now
	return r.db.WithContext(ctx).Create(memory).Error
}

// GetByID retrieves a single memory by ID and tenant.
func (r *MemoryRepository) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	var m types.AgentMemory
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Update persists changes to a memory. If the memory has a protected verdict
// (decision, fixed) and the context actor is "dreamer" or "system", the
// update is rejected with ErrProtectedVerdict.
func (r *MemoryRepository) Update(ctx context.Context, memory *types.AgentMemory) error {
	if err := r.checkProtectedVerdict(ctx, memory); err != nil {
		return err
	}
	memory.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Save(memory).Error
}

// checkProtectedVerdict rejects updates to protected verdicts by automated actors.
func (r *MemoryRepository) checkProtectedVerdict(ctx context.Context, memory *types.AgentMemory) error {
	// Only check if the memory ID is set
	if memory.ID == "" {
		return nil
	}

	existing, err := r.GetByID(ctx, memory.TenantID, memory.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // new memory, no check needed
		}
		return err
	}

	// If the existing memory does NOT have a protected verdict, allow the update
	if !existing.Verdict.IsProtected() {
		return nil
	}

	// Check the actor stored in context
	actor, _ := ctx.Value("actor").(string)
	if actor == "dreamer" || actor == "system" {
		return &types.ErrProtectedVerdict{}
	}
	return nil
}

// Delete soft-deletes a memory.
func (r *MemoryRepository) Delete(ctx context.Context, tenantID, id string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.AgentMemory{}).Error
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// Search returns memories matching the given filter, ordered by created_at
// descending. When Verdicts is nil/empty, refuted memories are excluded.
func (r *MemoryRepository) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	// Count
	var total int64
	countQuery := r.buildSearchQuery(ctx, filter)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*types.MemorySearchResult{}, 0, nil
	}

	// Fetch
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	fetchQuery := r.buildSearchQuery(ctx, filter)
	var memories []*types.AgentMemory
	if err := fetchQuery.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&memories).Error; err != nil {
		return nil, 0, err
	}

	results := make([]*types.MemorySearchResult, len(memories))
	for i, m := range memories {
		results[i] = &types.MemorySearchResult{Memory: m}
	}
	return results, total, nil
}

// buildSearchQuery constructs a *gorm.DB with search conditions applied.
func (r *MemoryRepository) buildSearchQuery(ctx context.Context, filter *types.MemoryFilter) *gorm.DB {
	db := r.db.WithContext(ctx).Table("agent_memories").
		Where("tenant_id = ?", filter.TenantID).
		Where("deleted_at IS NULL")

	// Default: exclude refuted
	if len(filter.Verdicts) == 0 {
		db = db.Where("verdict != ?", types.VerdictRefuted)
	} else {
		db = db.Where("verdict IN ?", filter.Verdicts)
	}

	if filter.MemoryType != "" {
		db = db.Where("memory_type = ?", filter.MemoryType)
	}
	if filter.Tier != nil {
		db = db.Where("tier = ?", *filter.Tier)
	}
	if filter.SessionID != "" {
		db = db.Where("session_id = ?", filter.SessionID)
	}
	if filter.Query != "" {
		db = db.Where("content LIKE ?", "%"+filter.Query+"%")
	}

	return db
}

// ---------------------------------------------------------------------------
// CosineSearch (pgvector HNSW)
// ---------------------------------------------------------------------------

// cosineRow is a scan target for CosineSearch results.
type cosineRow struct {
	ID          string
	TenantID    string
	Content     string
	MemoryType  string             `gorm:"column:memory_type"`
	Importance  int
	Tier        int
	Verdict     types.MemoryVerdict
	HubScore    float64            `gorm:"column:hub_score"`
	AccessCount int                `gorm:"column:access_count"`
	SessionID   string             `gorm:"column:session_id"`
	CreatedAt   time.Time          `gorm:"column:created_at"`
	UpdatedAt   time.Time          `gorm:"column:updated_at"`
	CosineScore float64            `gorm:"column:cosine_score"`
}

// CosineSearch performs vector similarity search using pgvector's cosine
// distance operator (<=>). Results are ordered by similarity and include a
// cosine_score column (1 - cosine_distance). Tenant isolation and verdict
// filtering are applied.
func (r *MemoryRepository) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	vec := pgvector.NewVector(embedding)
	db := r.db.WithContext(ctx)

	// Build the query using db.Table for raw column list control.
	query := db.Table("agent_memories").
		Select("id, tenant_id, content, memory_type, importance, tier, verdict, hub_score, access_count, session_id, created_at, updated_at, 1 - (embedding <=> ?) AS cosine_score", vec).
		Where("tenant_id = ?", filter.TenantID).
		Where("deleted_at IS NULL").
		Clauses(clause.OrderBy{
			Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []interface{}{vec}},
		}).
		Limit(limit)

	// Default: exclude refuted
	if len(filter.Verdicts) == 0 {
		query = query.Where("verdict != ?", types.VerdictRefuted)
	} else if len(filter.Verdicts) > 0 {
		query = query.Where("verdict IN ?", filter.Verdicts)
	}

	var rows []cosineRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	results := make([]*types.MemorySearchResult, len(rows))
	for i, row := range rows {
		results[i] = &types.MemorySearchResult{
			Memory: &types.AgentMemory{
				ID:          row.ID,
				TenantID:    row.TenantID,
				Content:     row.Content,
				MemoryType:  row.MemoryType,
				Importance:  row.Importance,
				Tier:        row.Tier,
				Verdict:     row.Verdict,
				HubScore:    row.HubScore,
				AccessCount: row.AccessCount,
				SessionID:   row.SessionID,
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			},
			Score: row.CosineScore,
		}
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Dreamer gate
// ---------------------------------------------------------------------------

// TryDreamerLock attempts to acquire the dreamer lock for a tenant.
// Returns true if the lock was acquired, false if it is held by another worker.
func (r *MemoryRepository) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	var acquired bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state types.DreamerState
		result := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ?", tenantID).
			First(&state)

		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// No lock exists — create one
			newState := &types.DreamerState{
				ID:          uuid.New().String(),
				TenantID:    tenantID,
				LockedBy:    workerID,
				LockedUntil: timePtr(time.Now().Add(10 * time.Minute)),
				LastRunAt:   timePtr(time.Now()),
			}
			if err := tx.Create(newState).Error; err != nil {
				return err
			}
			acquired = true
			return nil
		}
		if result.Error != nil {
			return result.Error
		}

		// Check if existing lock is expired
		if state.LockedUntil == nil || state.LockedUntil.Before(time.Now()) {
			updates := map[string]interface{}{
				"locked_by":    workerID,
				"locked_until": time.Now().Add(10 * time.Minute),
				"last_run_at":  time.Now(),
			}
			if err := tx.Model(&state).Updates(updates).Error; err != nil {
				return err
			}
			acquired = true
			return nil
		}

		// Lock is still held by another worker
		acquired = false
		return nil
	})
	return acquired, err
}

// UnlockDreamer releases the dreamer lock for a tenant.
func (r *MemoryRepository) UnlockDreamer(ctx context.Context, tenantID string) error {
	return r.db.WithContext(ctx).
		Model(&types.DreamerState{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{
			"locked_by":    "",
			"locked_until": nil,
		}).Error
}

// ---------------------------------------------------------------------------
// Hub scores
// ---------------------------------------------------------------------------

// degreeStat is a scan target for ComputeHubScores.
type degreeStat struct {
	MemoryID  string  `gorm:"column:memory_id"`
	Degree    int64   `gorm:"column:degree"`
	AvgWeight float64 `gorm:"column:avg_weight"`
}

// ComputeHubScores recomputes hub_scores for all memories in a tenant by
// counting in-degree and out-degree edges from memory_relations and applying
// the formula: hub_score = LN(1 + degree) * avg_weight.
func (r *MemoryRepository) ComputeHubScores(ctx context.Context, tenantID string) error {
	var stats []degreeStat
	err := r.db.WithContext(ctx).Raw(`
		SELECT memory_id, COUNT(*) AS degree, COALESCE(AVG(weight), 0) AS avg_weight
		FROM (
			SELECT from_uuid AS memory_id, weight
			FROM memory_relations
			WHERE deleted_at IS NULL AND tenant_id = ?
			UNION ALL
			SELECT to_uuid AS memory_id, weight
			FROM memory_relations
			WHERE deleted_at IS NULL AND tenant_id = ?
		) all_edges
		GROUP BY memory_id
	`, tenantID, tenantID).Scan(&stats).Error
	if err != nil {
		return err
	}

	for _, stat := range stats {
		hubScore := types.HubScoreFromDegree(float64(stat.Degree), stat.AvgWeight)
		if err := r.db.WithContext(ctx).
			Model(&types.AgentMemory{}).
			Where("id = ? AND tenant_id = ?", stat.MemoryID, tenantID).
			Updates(map[string]interface{}{
				"hub_score":  hubScore,
				"updated_at": time.Now(),
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// InvalidateResultCache is a no-op stub. Full cache invalidation will be
// implemented when the query result cache is added in Phase 2.
func (r *MemoryRepository) InvalidateResultCache(_ context.Context, _ string) {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func timePtr(t time.Time) *time.Time { return &t }
