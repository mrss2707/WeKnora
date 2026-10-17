package memory_v2

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// rrfK is the Reciprocal Rank Fusion constant: fused = sum 1/(rrfK + rank).
// RRF needs no score calibration between BM25 and cosine, which live on
// unrelated scales.
const rrfK = 60

// ErrEmbeddingDimensionMismatch is returned when the query embedding dimension
// does not match the stored embedding dimension, indicating a model mismatch.
var ErrEmbeddingDimensionMismatch = fmt.Errorf("embedding dimension mismatch: query embedding model differs from the model used for stored memories; re-ingest memories with the current embedding model")

// SearchMemories performs the hybrid search pipeline: BM25 keyword ranking
// and HNSW vector ranking (both with every filter applied) fused by
// Reciprocal Rank Fusion, then importance/hub/verdict/recency/session boosts,
// then a vector-similarity floor and a floor relative to the best result.
func (s *MemoryServiceV2Impl) SearchMemories(ctx context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	if filter == nil {
		return nil, fmt.Errorf("filter is required")
	}
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2 not ready: %s", reason)
	}
	if filter.Limit <= 0 {
		filter.Limit = s.config.MaxSearchResults
	}
	if strings.TrimSpace(query) == "" {
		// No query: the newest memories matching the filter, unranked.
		f := *filter
		f.Query = ""
		results, _, err := s.repo.Search(ctx, &f)
		return results, err
	}
	candidates := filter.Limit * 3

	// Step 1: Keyword ranking (BM25)
	dbStarted := time.Now()
	bm25Results, err := s.fullTextSearch(ctx, query, filter, candidates)
	telemetry.Observe("recall_keyword", "database", dbStarted)
	if err != nil {
		logger.Errorf(ctx, "full-text search failed: %v", err)
		bm25Results = nil
	}

	// Step 2: Vector ranking (requires embedding)
	var cosineResults []*types.MemorySearchResult
	if strings.TrimSpace(query) != "" {
		cosineResults = s.vectorSearch(ctx, query, filter, candidates)
	}

	// Step 3: Fuse rankings, drop weak vector-only matches
	merged := s.fuseRankings(bm25Results, cosineResults)

	// Step 4: Boosts
	merged = s.applySignalBoost(merged)
	merged = s.applyRecencyBoost(merged)
	merged = s.applyVerdictFilter(merged, filter)
	merged = s.applySessionBoost(merged, filter.SessionID)

	// Step 5: Sort, keep results close to the best one, limit
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	merged = s.applyScoreThreshold(merged)
	if filter.MinScore > 0 {
		merged = keepAtLeast(merged, filter.MinScore)
	}
	if len(merged) > filter.Limit {
		merged = merged[:filter.Limit]
	}

	// Compute staleness for each result
	for _, r := range merged {
		if r.Memory != nil {
			age := time.Since(r.Memory.CreatedAt)
			r.StaleDays = int(age.Hours() / 24)
			r.IsStale = r.StaleDays > 180 && r.Memory.Importance < 1
		}
	}

	s.trackAccess(ctx, filter.TenantID, merged)
	return merged, nil
}

// vectorSearch embeds the query with the scope's model and runs the filtered
// vector search. Failures degrade to keyword-only results.
func (s *MemoryServiceV2Impl) vectorSearch(ctx context.Context, query string, filter *types.MemoryFilter, limit int) []*types.MemorySearchResult {
	embedder, modelID, err := s.embedderFor(ctx, filter.TenantID, filter.KbID)
	if err != nil {
		logger.Errorf(ctx, "embedder not available: %v", err)
		return nil
	}
	modelStarted := time.Now()
	vector, err := embedder.Embed(ctx, query)
	telemetry.Observe("recall_embedding", "model", modelStarted)
	if err != nil {
		logger.Errorf(ctx, "embedding query failed: %v", err)
		return nil
	}
	if len(vector) == 0 {
		return nil
	}
	f := *filter
	f.Query = ""
	f.EmbeddingModelID = modelID
	dbStarted := time.Now()
	results, err := s.repo.CosineSearch(ctx, &f, vector, limit)
	telemetry.Observe("recall_vector", "database", dbStarted)
	if err != nil {
		if isDimensionMismatch(err) {
			logger.Warnf(ctx, "[MemoryV2] Embedding dimension mismatch: %v. "+
				"Stored memories were embedded with a different model. "+
				"Re-ingest memories or switch to the original embedding model.", err)
		} else {
			logger.Errorf(ctx, "cosine search failed: %v", err)
		}
		return nil
	}
	return results
}

// trackAccess records, off the request path, that these memories were
// returned (feeds pruning and staleness decisions).
func (s *MemoryServiceV2Impl) trackAccess(ctx context.Context, tenantID string, results []*types.MemorySearchResult) {
	tracker, ok := s.repo.(interfaces.MemoryV2AccessTracker)
	if !ok || tenantID == "" || len(results) == 0 {
		return
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		if r.Memory != nil && r.Memory.ID != "" && r.Memory.TenantID == tenantID {
			ids = append(ids, r.Memory.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	go func() {
		defer cancel()
		if err := tracker.TouchAccess(bg, tenantID, ids); err != nil {
			logger.Warnf(bg, "[MemoryV2] access tracking failed: %v", err)
		}
	}()
}

// fullTextSearch ranks memories by keyword relevance (BM25 when the
// repository supports it, otherwise keyword matches in recency order).
func (s *MemoryServiceV2Impl) fullTextSearch(ctx context.Context, query string, filter *types.MemoryFilter, limit int) ([]*types.MemorySearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	f := *filter
	f.Query = query
	f.Limit = limit
	f.Offset = 0
	if ranker, ok := s.repo.(interfaces.MemoryV2KeywordRanker); ok {
		return ranker.BM25Search(ctx, &f)
	}
	results, _, err := s.repo.Search(ctx, &f)
	return results, err
}

// fuseRankings merges the keyword and vector rankings with Reciprocal Rank
// Fusion. The fused score is normalized so a memory ranked first by both
// signals scores 1. Vector-only matches below MinCosine are dropped: they
// share no keyword with the query and are not similar enough to be relevant.
func (s *MemoryServiceV2Impl) fuseRankings(bm25, cosine []*types.MemorySearchResult) []*types.MemorySearchResult {
	type entry struct {
		result  *types.MemorySearchResult
		fused   float64
		keyword bool
		cosine  float64
	}
	byID := map[string]*entry{}
	var order []string
	add := func(r *types.MemorySearchResult, rank int, keyword bool) {
		if r == nil || r.Memory == nil || r.Memory.ID == "" {
			return
		}
		e, ok := byID[r.Memory.ID]
		if !ok {
			e = &entry{result: r}
			byID[r.Memory.ID] = e
			order = append(order, r.Memory.ID)
		}
		e.fused += 1.0 / float64(rrfK+rank+1)
		if keyword {
			e.keyword = true
		} else {
			e.cosine = r.Score
			// Prefer the vector result's projection: it carries hub_score.
			e.result = r
		}
	}
	for i, r := range bm25 {
		add(r, i, true)
	}
	for i, r := range cosine {
		add(r, i, false)
	}

	minCos := s.config.MinCosine
	best := 2.0 / float64(rrfK+1)
	out := make([]*types.MemorySearchResult, 0, len(order))
	for _, id := range order {
		e := byID[id]
		if !e.keyword && e.cosine < minCos {
			continue
		}
		e.result.Score = e.fused / best
		out = append(out, e.result)
	}
	return out
}

// applySignalBoost scales scores by stored importance and graph centrality.
func (s *MemoryServiceV2Impl) applySignalBoost(results []*types.MemorySearchResult) []*types.MemorySearchResult {
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		imp := float64(clampInt(r.Memory.Importance, minImportance, maxImportance))
		r.Score *= 1 + 0.05*imp
		r.Score *= 1 + 0.1*math.Min(math.Max(r.Memory.HubScore, 0), 3)
	}
	return results
}

// keepAtLeast drops results scoring below min.
func keepAtLeast(results []*types.MemorySearchResult, min float64) []*types.MemorySearchResult {
	out := results[:0]
	for _, r := range results {
		if r.Score >= min {
			out = append(out, r)
		}
	}
	return out
}

// applyRecencyBoost applies a 2-tier recency boost.
func (s *MemoryServiceV2Impl) applyRecencyBoost(results []*types.MemorySearchResult) []*types.MemorySearchResult {
	if !s.config.RecencyBoost.Enabled {
		return results
	}

	now := time.Now()
	shortWindow, _ := time.ParseDuration(s.config.RecencyBoost.ShortTermWindow)
	if shortWindow <= 0 {
		shortWindow = time.Hour
	}
	halfLife := s.config.RecencyBoost.LongTermHalfLife
	if halfLife <= 0 {
		halfLife = 30
	}

	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		age := now.Sub(r.Memory.CreatedAt)

		if age <= shortWindow {
			// Short-term: multiply by short-term multiplier (default 1.15)
			r.Score *= s.config.RecencyBoost.ShortTermMultiplier
		} else {
			// Long-term: exponential decay
			days := age.Hours() / 24
			factor := math.Exp(-float64(days) / float64(halfLife) * s.config.RecencyBoost.LongTermFactor)
			r.Score *= (1 + factor) / 2
		}
	}

	return results
}

// applyVerdictFilter applies verdict-based filtering and boosts.
func (s *MemoryServiceV2Impl) applyVerdictFilter(results []*types.MemorySearchResult, filter *types.MemoryFilter) []*types.MemorySearchResult {
	var filtered []*types.MemorySearchResult

	for _, r := range results {
		if r.Memory == nil {
			continue
		}

		// Default: exclude refuted
		if len(filter.Verdicts) == 0 && r.Memory.Verdict == types.VerdictRefuted {
			continue
		}

		// Boost decision verdicts
		if r.Memory.Verdict == types.VerdictDecision {
			r.Score *= 1.2
		}

		// Boost fixed verdicts
		if r.Memory.Verdict == types.VerdictFixed {
			r.Score *= 1.1
		}

		filtered = append(filtered, r)
	}

	return filtered
}

// applySessionBoost boosts results from the same session.
func (s *MemoryServiceV2Impl) applySessionBoost(results []*types.MemorySearchResult, sessionID string) []*types.MemorySearchResult {
	if sessionID == "" {
		return results
	}

	for _, r := range results {
		if r.Memory != nil && r.Memory.SessionID == sessionID {
			r.Score *= 1.3
		}
	}

	return results
}

// applyScoreThreshold keeps results scoring at least RelativeScoreFloor of
// the best result. Results must be sorted by score, best first.
func (s *MemoryServiceV2Impl) applyScoreThreshold(results []*types.MemorySearchResult) []*types.MemorySearchResult {
	if len(results) == 0 {
		return results
	}
	ratio := s.config.RelativeScoreFloor
	if ratio <= 0 {
		return results
	}
	floor := results[0].Score * ratio
	out := results[:0]
	for _, r := range results {
		if r.Score >= floor {
			out = append(out, r)
		}
	}
	return out
}

// extractKeywordsFromQuery extracts important words from a query for BM25 search.
func extractKeywordsFromQuery(query string) string {
	words := strings.Fields(query)
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:\"'")
		if len(w) > 2 && !isStopWord(strings.ToLower(w)) {
			keywords = append(keywords, w)
		}
	}
	if len(keywords) == 0 {
		return query
	}
	return strings.Join(keywords, " ")
}

// isDimensionMismatch detects pgvector dimension mismatch errors.
// PG returns: "ERROR: different vector dimensions 768 and 2000 (SQLSTATE 22000)"
func isDimensionMismatch(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "different vector dimensions")
}
