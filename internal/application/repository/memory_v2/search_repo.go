package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// memoryColumns is the projection every search returns (no embedding).
const memoryColumns = "id, tenant_id, kb_id, user_id, content, memory_type, importance, tier, verdict, " +
	"hub_score, access_count, session_id, tags, last_accessed_at, expires_at, created_at, updated_at"

// scoredRow is the scan target for ranked searches.
type scoredRow struct {
	types.AgentMemory
	Score float64 `gorm:"column:score"`
}

// applyFilters adds every MemoryFilter condition shared by list, keyword and
// vector search, so no branch can drift from the others. The query text and
// the embedding-model condition are added by the caller.
func applyFilters(db *gorm.DB, filter *types.MemoryFilter) *gorm.DB {
	db = db.Where("tenant_id = ?", filter.TenantID).Where("deleted_at IS NULL")
	if filter.KbID != "" {
		db = db.Where("kb_id = ?", filter.KbID)
	}
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
	if filter.AuthorUserID != "" {
		db = db.Where("user_id = ?", filter.AuthorUserID)
	}
	return db
}

// paradeDBAvailable checks for the pg_search extension once and caches it.
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

// keywordCondition matches any query term: ParadeDB BM25 (`match` is a
// disjunction over the analyzed terms and accepts arbitrary user text), or
// PostgreSQL full-text search with the language-neutral `simple` config.
func (r *MemoryRepository) keywordCondition(ctx context.Context, query string) (string, []interface{}) {
	if r.paradeDBAvailable(ctx) {
		return "id @@@ paradedb.match('content', ?)", []interface{}{query}
	}
	return "to_tsvector('simple', content) @@ " + orTSQuery, []interface{}{query}
}

// orTSQuery turns plain text into an OR tsquery (plainto_tsquery ANDs every
// word, which drops most natural-language matches).
const orTSQuery = "replace(plainto_tsquery('simple', ?)::text, ' & ', ' | ')::tsquery"

// Search lists memories matching the filter, newest first, with the total
// count. A non-empty Query keeps only memories containing any query term.
func (r *MemoryRepository) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	build := func() *gorm.DB {
		db := applyFilters(r.db.WithContext(ctx).Table("agent_memories"), filter)
		if q := strings.TrimSpace(filter.Query); q != "" {
			cond, args := r.keywordCondition(ctx, q)
			db = db.Where(cond, args...)
		}
		return db
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*types.MemorySearchResult{}, 0, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	var memories []*types.AgentMemory
	if err := build().Select(memoryColumns).Order("created_at DESC, id").
		Limit(limit).Offset(offset).Find(&memories).Error; err != nil {
		return nil, 0, err
	}
	results := make([]*types.MemorySearchResult, len(memories))
	for i, m := range memories {
		results[i] = &types.MemorySearchResult{Memory: m}
	}
	return results, total, nil
}

// BM25Search ranks memories by keyword relevance (ParadeDB BM25, or ts_rank
// over the `simple` config), applying every filter.
func (r *MemoryRepository) BM25Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	q := strings.TrimSpace(filter.Query)
	if q == "" {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	db := applyFilters(r.db.WithContext(ctx).Table("agent_memories"), filter)
	if r.paradeDBAvailable(ctx) {
		db = db.Select(memoryColumns+", paradedb.score(id) AS score").
			Where("id @@@ paradedb.match('content', ?)", q).
			Order("score DESC, id")
	} else {
		db = db.Select(memoryColumns+", ts_rank(to_tsvector('simple', content), "+orTSQuery+") AS score", q).
			Where("to_tsvector('simple', content) @@ "+orTSQuery, q).
			Order("score DESC, id")
	}
	var rows []scoredRow
	if err := db.Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return toResults(rows), nil
}

// CosineSearch ranks memories by vector similarity (pgvector HNSW), applying
// every filter. The score is 1 - cosine distance. HNSW is tuned per query so
// a selective filter (one knowledge base among many tenants) does not starve
// the result: ef_search is raised and iterative scan keeps pulling candidates
// until enough rows pass the filter.
func (r *MemoryRepository) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	padded, err := padEmbedding(embedding)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	vec := pgvector.NewVector(padded)
	build := func(tx *gorm.DB) *gorm.DB {
		q := applyFilters(tx.Table("agent_memories"), filter).
			Where("embedding IS NOT NULL").
			Select(memoryColumns+", 1 - (embedding <=> ?) AS score", vec)
		if filter.EmbeddingModelID != "" {
			// Unknown legacy vectors are not evidence of model compatibility.
			// Admin relink re-embeds them; keyword recall remains available.
			q = q.Where("metadata->>'embedding_model_id' = ?", filter.EmbeddingModelID)
		}
		return q.Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "embedding <=> ?", Vars: []interface{}{vec}}}).Limit(limit)
	}

	efSearch := limit * 4
	if efSearch < 100 {
		efSearch = 100
	}
	if efSearch > 1000 {
		efSearch = 1000
	}
	var rows []scoredRow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch)).Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL hnsw.iterative_scan = relaxed_order").Error; err != nil {
			return err
		}
		return build(tx).Scan(&rows).Error
	})
	if err != nil && (strings.Contains(err.Error(), "hnsw.") || strings.Contains(err.Error(), "unrecognized configuration")) {
		// Older pgvector without these settings: run with defaults.
		logger.Warnf(ctx, "[MemoryV2] HNSW tuning unavailable, using defaults: %v", err)
		rows = nil
		err = build(r.db.WithContext(ctx)).Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	results := toResults(rows)
	// relaxed_order may return rows slightly out of order; restore it.
	sortByScoreDesc(results)
	return results, nil
}

func toResults(rows []scoredRow) []*types.MemorySearchResult {
	out := make([]*types.MemorySearchResult, len(rows))
	for i := range rows {
		m := rows[i].AgentMemory
		out[i] = &types.MemorySearchResult{Memory: &m, Score: rows[i].Score}
	}
	return out
}

func sortByScoreDesc(rs []*types.MemorySearchResult) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Score > rs[j].Score })
}
