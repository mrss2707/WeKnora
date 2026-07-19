package memory_v2

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Hybrid search weights for merging signals.
const (
	weightBM25     = 0.15
	weightCosine   = 0.45
	weightHubScore = 0.25
	weightImpor    = 0.15
)

// SearchMemories performs the hybrid search pipeline:
// BM25 full-text search + HNSW cosine search + hub_score + importance scoring,
// merges with configured weights, applies boosts and filters.
func (s *MemoryServiceV2Impl) SearchMemories(ctx context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	if filter == nil {
		return nil, fmt.Errorf("filter is required")
	}
	if filter.Limit <= 0 {
		filter.Limit = s.config.MaxSearchResults
	}

	// Step 1: Run full-text search (BM25 via postgres text search)
	bm25Results, err := s.fullTextSearch(ctx, query, filter)
	if err != nil {
		logger.Errorf(ctx, "full-text search failed: %v", err)
		bm25Results = nil
	}

	// Step 2: Run cosine vector search (requires embedding)
	var cosineResults []*types.MemorySearchResult
	if query != "" {
		embedder, err := s.getEmbedder(ctx)
		if err != nil {
			logger.Errorf(ctx, "embedder not available: %v", err)
		} else {
			vector, err := embedder.Embed(ctx, query)
			if err != nil {
				logger.Errorf(ctx, "embedding query failed: %v", err)
			} else if len(vector) > 0 {
				cosineFilter := &types.MemoryFilter{
					TenantID: filter.TenantID,
					Verdicts: filter.Verdicts,
				}
				cosineResults, err = s.repo.CosineSearch(ctx, cosineFilter, vector, filter.Limit*2)
				if err != nil {
					logger.Errorf(ctx, "cosine search failed: %v", err)
					cosineResults = nil
				}
			}
		}
	}

	// Step 3: Merge results with weighted scoring
	merged := s.mergeResults(bm25Results, cosineResults, filter.Limit)

	// Step 4: Apply recency boost
	merged = s.applyRecencyBoost(merged)

	// Step 5: Apply verdict filtering and boost
	merged = s.applyVerdictFilter(merged, filter)

	// Step 6: Apply session boost
	merged = s.applySessionBoost(merged, filter.SessionID)

	// Step 7: Apply score threshold
	merged = s.applyScoreThreshold(ctx, merged)

	// Step 8: Sort by score descending and limit
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
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

	return merged, nil
}

// fullTextSearch runs BM25-like full text search using postgres text search via the repo.
func (s *MemoryServiceV2Impl) fullTextSearch(ctx context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	searchFilter := &types.MemoryFilter{
		TenantID:   filter.TenantID,
		UserID:     filter.UserID,
		Query:      query,
		MemoryType: filter.MemoryType,
		Tier:       filter.Tier,
		Verdicts:   filter.Verdicts,
		SessionID:  filter.SessionID,
		Limit:      filter.Limit * 2,
	}

	results, _, err := s.repo.Search(ctx, searchFilter)
	return results, err
}

// mergeResults combines BM25 and cosine results with weighted scoring.
func (s *MemoryServiceV2Impl) mergeResults(bm25, cosine []*types.MemorySearchResult, limit int) []*types.MemorySearchResult {
	if len(bm25) == 0 && len(cosine) == 0 {
		return nil
	}
	if len(bm25) == 0 {
		// Only cosine: use cosine scores directly
		for _, r := range cosine {
			r.Score = r.Score * weightCosine
		}
		return cosine
	}
	if len(cosine) == 0 {
		// Only BM25: assign heuristic score based on position
		for i, r := range bm25 {
			r.Score = float64(len(bm25)-i) / float64(len(bm25)) * weightBM25
		}
		return bm25
	}

	// Build score map from cosine results
	cosineScores := make(map[string]float64)
	cosineImportance := make(map[string]int)
	cosineHubScore := make(map[string]float64)
	for _, r := range cosine {
		if r.Memory != nil {
			cosineScores[r.Memory.ID] = r.Score
			cosineImportance[r.Memory.ID] = r.Memory.Importance
			cosineHubScore[r.Memory.ID] = r.Memory.HubScore
		}
	}

	// Track seen IDs to avoid duplicates
	seen := make(map[string]bool)
	var merged []*types.MemorySearchResult

	if len(bm25) > 0 {
	}

	// Process BM25 results first
	for i, r := range bm25 {
		if r.Memory == nil {
			continue
		}
		id := r.Memory.ID
		seen[id] = true

		bm25score := float64(len(bm25)-i) / float64(len(bm25))

		cosScore := cosineScores[id]
		impScore := float64(cosineImportance[id]+5) / 11.0 // Normalize [-5,6] to [0,1]
		if impScore > 1 {
			impScore = 1
		}
		if impScore < 0 {
			impScore = 0
		}
		hubScore := math.Min(cosineHubScore[id]/5.0, 1.0)

		r.Score = bm25score*weightBM25 + cosScore*weightCosine + hubScore*weightHubScore + impScore*weightImpor
		merged = append(merged, r)
	}

	// Add cosine-only results
	for _, r := range cosine {
		if r.Memory == nil || seen[r.Memory.ID] {
			continue
		}
		seen[r.Memory.ID] = true

		impScore := float64(r.Memory.Importance+5) / 11.0
		if impScore > 1 {
			impScore = 1
		}
		if impScore < 0 {
			impScore = 0
		}
		hubScore := math.Min(r.Memory.HubScore/5.0, 1.0)

		r.Score = r.Score*weightCosine + hubScore*weightHubScore + impScore*weightImpor
		merged = append(merged, r)
	}

	return merged
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

// applyScoreThreshold discards results below the configured minimum score.
func (s *MemoryServiceV2Impl) applyScoreThreshold(ctx context.Context, results []*types.MemorySearchResult) []*types.MemorySearchResult {
	threshold := s.config.MinScoreThreshold
	if threshold <= 0 {
		threshold = 0.4
	}

	var filtered []*types.MemorySearchResult
	for _, r := range results {
		if r.Score >= threshold {
			filtered = append(filtered, r)
		}
	}

	_ = ctx
	return filtered
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
