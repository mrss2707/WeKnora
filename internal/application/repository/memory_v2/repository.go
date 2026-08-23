package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MemoryRepository implements MemoryRepositoryV2 using GORM + PostgreSQL/pgvector.
type MemoryRepository struct {
	db            *gorm.DB
	cache         interfaces.CacheInvalidator
	paradeDBOnce  sync.Once
	paradeDBAvail bool
}

// NewMemoryRepository creates a new MemoryRepository.
func NewMemoryRepository(db *gorm.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// SetCacheInvalidator injects the cache invalidator.
func (r *MemoryRepository) SetCacheInvalidator(cache interfaces.CacheInvalidator) {
	r.cache = cache
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
	// The embedding column is declared vector(MemoryEmbeddingDim) and pgvector
	// typmods require an exact dimension match. Every embedding — from any
	// model with ≤MemoryEmbeddingDim dims — is zero-padded to the declared
	// width here (cosine/L2 distances are padding-invariant); larger models
	// are rejected so the failure is a clear error instead of a PG insert error.
	padded, err := padEmbedding(memory.Embedding.Slice())
	if err != nil {
		return err
	}
	memory.Embedding = pgvector.NewVector(padded)
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

// GetByFingerprint retrieves a memory by its content fingerprint.
func (r *MemoryRepository) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	var m types.AgentMemory
	err := r.db.WithContext(ctx).
		Where("fingerprint = ? AND tenant_id = ? AND deleted_at IS NULL", fingerprint, tenantID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
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
	actor, _ := ctx.Value(types.ActorKey{}).(string)
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
// Relations
// ---------------------------------------------------------------------------

// CreateRelation inserts a new relation, ignoring on conflict (unique constraint).
func (r *MemoryRepository) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	if rel.ID == "" {
		rel.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(rel).Error
}

// GetRelations returns all relations connected to a memory (both directions).
func (r *MemoryRepository) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	var rels []*types.MemoryRelation
	err := r.db.WithContext(ctx).
		Where("(from_uuid = ? OR to_uuid = ?) AND tenant_id = ? AND deleted_at IS NULL", memoryID, memoryID, tenantID).
		Find(&rels).Error
	return rels, err
}

// DeleteRelation hard-deletes a relation by ID and tenant.
func (r *MemoryRepository) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&types.MemoryRelation{}).Error
}

// HardDeleteExpired permanently deletes soft-deleted memories past the retention
// window. Only removes entries with access_count=0 to preserve cold-but-used data.
func (r *MemoryRepository) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Unscoped().
		Where("tenant_id = ? AND deleted_at IS NOT NULL AND deleted_at < ? AND access_count = 0", tenantID, olderThan).
		Delete(&types.AgentMemory{})
	return result.RowsAffected, result.Error
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

	if filter.KbID != "" {
		db = db.Where("kb_id = ?", filter.KbID)
	}

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
		if r.paradeDBAvailable(ctx) {
			db = db.Where("content @@@ paradedb.phrase(field => 'content', phrase => ?)", filter.Query)
		} else {
			db = db.Where("to_tsvector('english', content) @@ plainto_tsquery('english', ?)", filter.Query)
		}
	}

	return db
}

// paradeDBAvailable checks for pg_search extension once and caches the result.
func (r *MemoryRepository) paradeDBAvailable(ctx context.Context) bool {
	r.paradeDBOnce.Do(func() {
		var count int
		if err := r.db.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM pg_extension WHERE extname = 'pg_search'",
		).Scan(&count).Error; err == nil && count > 0 {
			r.paradeDBAvail = true
		}
	})
	return r.paradeDBAvail
}

// BM25Search performs full-text search using ParadeDB or PostgreSQL tsvector fallback.
func (r *MemoryRepository) BM25Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	if filter.Query == "" {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	if r.paradeDBAvailable(ctx) {
		return r.bm25Raw(ctx, filter, limit, true)
	}
	return r.bm25Raw(ctx, filter, limit, false)
}

// bm25Row is a scan target for BM25 search results.
type bm25Row struct {
	ID          string            `gorm:"column:id"`
	TenantID    string            `gorm:"column:tenant_id"`
	Content     string            `gorm:"column:content"`
	MemoryType  string            `gorm:"column:memory_type"`
	Importance  int               `gorm:"column:importance"`
	Tier        int               `gorm:"column:tier"`
	Verdict     types.MemoryVerdict `gorm:"column:verdict"`
	HubScore    float64           `gorm:"column:hub_score"`
	AccessCount int               `gorm:"column:access_count"`
	SessionID   string            `gorm:"column:session_id"`
	CreatedAt   time.Time         `gorm:"column:created_at"`
	UpdatedAt   time.Time         `gorm:"column:updated_at"`
	BM25Score   float64           `gorm:"column:bm25_score"`
}

// bm25Raw executes the BM25 query (ParadeDB or tsvector path).
func (r *MemoryRepository) bm25Raw(ctx context.Context, filter *types.MemoryFilter, limit int, paradeDB bool) ([]*types.MemorySearchResult, error) {
	var rows []bm25Row
	var err error

	if paradeDB {
		q := r.db.WithContext(ctx).Table("agent_memories").
			Select("*, paradedb.score(id) AS bm25_score").
			Where("tenant_id = ?", filter.TenantID).
			Where("deleted_at IS NULL")
		if filter.KbID != "" {
			q = q.Where("kb_id = ?", filter.KbID)
		}
		err = q.Where("content @@@ paradedb.phrase(field => 'content', phrase => ?)", filter.Query).
			Order("bm25_score DESC").
			Limit(limit).
			Find(&rows).Error
	} else {
		q := r.db.WithContext(ctx).Table("agent_memories").
			Select("*, ts_rank(to_tsvector('english', content), plainto_tsquery('english', ?)) AS bm25_score", filter.Query).
			Where("tenant_id = ?", filter.TenantID).
			Where("deleted_at IS NULL")
		if filter.KbID != "" {
			q = q.Where("kb_id = ?", filter.KbID)
		}
		err = q.Where("to_tsvector('english', content) @@ plainto_tsquery('english', ?)", filter.Query).
			Order("bm25_score DESC").
			Limit(limit).
			Find(&rows).Error
	}
	if err != nil {
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
			Score: row.BM25Score,
		}
	}
	return results, nil
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
	// Pad the query vector to the column width so the <=> operator sees
	// matching dimensions (padding does not change cosine/L2 distances).
	padded, err := padEmbedding(embedding)
	if err != nil {
		return nil, err
	}
	vec := pgvector.NewVector(padded)
	db := r.db.WithContext(ctx)

	// Build the query using db.Table for raw column list control.
	query := db.Table("agent_memories").
		Select("id, tenant_id, content, memory_type, importance, tier, verdict, hub_score, access_count, session_id, created_at, updated_at, 1 - (embedding <=> ?) AS cosine_score", vec).
		Where("tenant_id = ?", filter.TenantID).
		Where("deleted_at IS NULL")

	if filter.KbID != "" {
		query = query.Where("kb_id = ?", filter.KbID)
	}

	query = query.Clauses(clause.OrderBy{
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

// InvalidateResultCache clears all cached entries matching the tenant prefix.
func (r *MemoryRepository) InvalidateResultCache(_ context.Context, tenantID string) {
	if r.cache != nil {
		r.cache.InvalidateByPrefix("mem:" + tenantID)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func timePtr(t time.Time) *time.Time { return &t }

// padEmbedding extends v to types.MemoryEmbeddingDim dimensions with trailing
// zeros and rejects inputs that exceed the column width. Both written rows and
// query vectors must sit exactly at the declared width because pgvector
// typmods reject vectors of any other dimensionality.
func padEmbedding(v []float32) ([]float32, error) {
	if len(v) == 0 {
		return make([]float32, types.MemoryEmbeddingDim), nil
	}
	if len(v) > types.MemoryEmbeddingDim {
		return nil, fmt.Errorf("embedding dimension %d exceeds the maximum supported dimension %d", len(v), types.MemoryEmbeddingDim)
	}
	if len(v) == types.MemoryEmbeddingDim {
		return v, nil
	}
	padded := make([]float32, types.MemoryEmbeddingDim)
	copy(padded, v)
	return padded, nil
}

// ensure MemoryRepository satisfies MemoryRepositoryV2 at compile time.
var _ interfaces.MemoryRepositoryV2 = (*MemoryRepository)(nil)

// GetEmbeddingDimension samples a single stored embedding row to determine the
// actual vector dimension. Returns (0, nil) when the table is empty.
func (r *MemoryRepository) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	var emb pgvector.Vector
	err := r.db.WithContext(ctx).
		Table("agent_memories").
		Select("embedding").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Limit(1).
		Scan(&emb).Error
	if err != nil {
		return 0, err
	}
	return len(emb.Slice()), nil
}
