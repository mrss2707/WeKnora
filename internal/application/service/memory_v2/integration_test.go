//go:build integration

package memory_v2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Integration test helpers
// ---------------------------------------------------------------------------

// mockIntegrationRepo implements MemoryRepositoryV2 for integration tests.
// It stores memories in-memory to simulate the real repository.
type mockIntegrationRepo struct {
	memories map[string]*types.AgentMemory
	sequences int
}

func newMockIntegrationRepo() *mockIntegrationRepo {
	return &mockIntegrationRepo{
		memories: make(map[string]*types.AgentMemory),
	}
}

func (r *mockIntegrationRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	r.sequences++
	if memory.ID == "" {
		memory.ID = fmt.Sprintf("int-mem-%d", r.sequences)
	}
	r.memories[memory.ID] = memory
	return nil
}

func (r *mockIntegrationRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	mem, ok := r.memories[id]
	if !ok {
		return nil, nil
	}
	return mem, nil
}

func (r *mockIntegrationRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	r.memories[memory.ID] = memory
	return nil
}

func (r *mockIntegrationRepo) Delete(ctx context.Context, tenantID, id string) error {
	delete(r.memories, id)
	return nil
}

func (r *mockIntegrationRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	var results []*types.MemorySearchResult
	for _, mem := range r.memories {
		if filter.TenantID != "" && mem.TenantID != filter.TenantID {
			continue
		}
		if filter.Query != "" && !strings.Contains(mem.Content, filter.Query) {
			continue
		}
		if filter.MemoryType != "" && mem.MemoryType != filter.MemoryType {
			continue
		}

		// Default: exclude refuted when no specific Verdicts filter
		if len(filter.Verdicts) == 0 && mem.Verdict == types.VerdictRefuted {
			continue
		}

		// When explicit verdicts are specified, only include matching memories
		if len(filter.Verdicts) > 0 {
			verdictMatch := false
			for _, v := range filter.Verdicts {
				if mem.Verdict == v {
					verdictMatch = true
					break
				}
			}
			if !verdictMatch {
				continue
			}
		}

		results = append(results, &types.MemorySearchResult{
			Memory: mem,
			Score:  0.0,
		})
	}

	// Limit
	if len(results) > filter.Limit && filter.Limit > 0 {
		results = results[:filter.Limit]
	}

	return results, int64(len(results)), nil
}

func (r *mockIntegrationRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	var results []*types.MemorySearchResult
	for _, mem := range r.memories {
		if filter.TenantID != "" && mem.TenantID != filter.TenantID {
			continue
		}

		// Default: exclude refuted when no specific Verdicts filter
		if len(filter.Verdicts) == 0 && mem.Verdict == types.VerdictRefuted {
			continue
		}

		// When explicit verdicts are specified, only include matching memories
		if len(filter.Verdicts) > 0 {
			verdictMatch := false
			for _, v := range filter.Verdicts {
				if mem.Verdict == v {
					verdictMatch = true
					break
				}
			}
			if !verdictMatch {
				continue
			}
		}

		results = append(results, &types.MemorySearchResult{
			Memory: mem,
			Score:  0.8, // constant mock similarity
		})
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (r *mockIntegrationRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	return true, nil
}

func (r *mockIntegrationRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	return nil
}

func (r *mockIntegrationRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	return nil
}

func (r *mockIntegrationRepo) InvalidateResultCache(ctx context.Context, tenantID string) {}

// mockIntegrationChat implements chat.Chat for the dreamer integration tests.
type mockIntegrationChat struct {
	response string
	err      error
}

func (m *mockIntegrationChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &types.ChatResponse{Content: m.response}, nil
}

func (m *mockIntegrationChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (m *mockIntegrationChat) GetModelName() string { return "mock" }
func (m *mockIntegrationChat) GetModelID() string   { return "mock-model" }

// newIntegrationService creates a fully-wired MemoryServiceV2Impl for integration tests.
func newIntegrationService(repo interfaces.MemoryRepositoryV2) *MemoryServiceV2Impl {
	config := types.DefaultMemoryV2Config()
	config.MinScoreThreshold = 0.0 // allow all scores through in integration tests
	config.LintOnWrite.Enabled = false

	svc := &MemoryServiceV2Impl{
		repo:        repo,
		embedder:    &mockEmbedder{},
		config:      config,
		tokenBudget: NewTokenBudgetManager(),
	}

	// Initialize workers to avoid nil pointer dereferences.
	svc.entityExtractor = workers.NewEntityExtractor(repo, nil)
	svc.autoLinker = workers.NewAutoLinker(repo, &mockEmbedder{})
	svc.consolidator = workers.NewConsolidator(repo, &mockEmbedder{})
	svc.dreamer = workers.NewDreamerWorker(repo, nil, config.Dreamer)
	svc.pruner = workers.NewPruner(repo)
	svc.healthChecker = workers.NewHealthChecker(repo)
	svc.cacheWarmer = workers.NewCacheWarmer(repo, &mockEmbedder{}, config.CacheWarmer)

	return svc
}

// ---------------------------------------------------------------------------
// Test: Save -> Search -> Retrieve
// ---------------------------------------------------------------------------

func TestMemoryV2Integration_SaveSearchRetrieve(t *testing.T) {
	repo := newMockIntegrationRepo()
	svc := newIntegrationService(repo)
	ctx := context.Background()

	// Save a memory through the ingestion pipeline
	memory := &types.AgentMemory{
		TenantID:   "tenant-save-1",
		Content:    "The user prefers clean architecture with dependency injection patterns throughout all services.",
		MemoryType: "preference",
	}

	result, err := svc.SaveMemory(ctx, memory)
	require.NoError(t, err, "SaveMemory should succeed")
	require.NotNil(t, result.Memory, "SaveMemoryResult should contain the memory")
	assert.True(t, result.Created, "memory should be created (not duplicate)")
	assert.NotEmpty(t, result.Memory.ID, "memory should have an ID assigned")
	assert.Equal(t, 0, len(result.LintIssues), "no lint issues expected (lint disabled)")

	// Verify the memory was stored in repo
	savedID := result.Memory.ID
	stored, err := repo.GetByID(ctx, "tenant-save-1", savedID)
	require.NoError(t, err)
	require.NotNil(t, stored, "memory should be retrievable from repo")
	assert.Equal(t, "The user prefers clean architecture with dependency injection patterns throughout all services.", stored.Content)
	assert.Equal(t, types.VerdictNone, stored.Verdict)
	assert.Equal(t, "preference", stored.MemoryType) // type detection preserved
	assert.NotZero(t, stored.Importance, "importance should be computed")

	// Search for the saved memory
	searchResults, err := svc.SearchMemories(ctx, "clean architecture dependency injection", &types.MemoryFilter{
		TenantID: "tenant-save-1",
		Limit:    10,
	})
	require.NoError(t, err, "SearchMemories should succeed")
	require.NotEmpty(t, searchResults, "saved memory should appear in search results")

	// Verify the saved memory appears in search results
	found := false
	for _, r := range searchResults {
		if r.Memory.ID == savedID {
			found = true
			assert.Greater(t, r.Score, 0.0, "matching memory should have a positive score")
			break
		}
	}
	assert.True(t, found, "saved memory should be found in search results")

	// Search with non-matching query should still return results (BM25 fallback)
	nonMatchResults, err := svc.SearchMemories(ctx, "zzzzyyyyxxxwwww", &types.MemoryFilter{
		TenantID: "tenant-save-1",
		Limit:    10,
	})
	require.NoError(t, err)
	// May or may not match, but no error should occur
	t.Logf("Non-matching search returned %d results", len(nonMatchResults))
}

// ---------------------------------------------------------------------------
// Test: MemoryContext bridge format
// ---------------------------------------------------------------------------

func TestMemoryV2Integration_MemoryContextBridge(t *testing.T) {
	repo := newMockIntegrationRepo()
	svc := newIntegrationService(repo)
	ctx := context.Background()

	// Save a memory
	memory := &types.AgentMemory{
		TenantID: "tenant-bridge-1",
		Content:  "The user decided to use PostgreSQL as the primary database for all microservices.",
	}
	_, err := svc.SaveMemory(ctx, memory)
	require.NoError(t, err, "SaveMemory should succeed")

	// RetrieveMemory should return the MemoryContext bridge format
	memCtx, err := svc.RetrieveMemory(ctx, "user-bridge-1", "PostgreSQL database")
	require.NoError(t, err, "RetrieveMemory should not error")
	require.NotNil(t, memCtx, "MemoryContext should not be nil")

	// Bridge format: MemoryContext with exactly one RelatedEpisode containing XML
	require.Len(t, memCtx.RelatedEpisodes, 1, "should have exactly one related episode")
	assert.Empty(t, memCtx.RelatedEntities, "related entities should be empty (not set by bridge)")
	assert.Empty(t, memCtx.RelatedRelations, "related relations should be empty (not set by bridge)")

	ep := memCtx.RelatedEpisodes[0]
	assert.Equal(t, "user-bridge-1", ep.UserID, "episode UserID should match the userID passed to RetrieveMemory")
	assert.Empty(t, ep.SessionID, "episode SessionID should be empty (not set by bridge)")

	// Summary should contain valid XML from packContext
	summary := ep.Summary
	assert.True(t, strings.HasPrefix(summary, "<memory_context>"), "summary should start with memory_context tag")
	assert.True(t, strings.HasSuffix(summary, "</memory_context>"), "summary should end with memory_context tag")
	assert.Contains(t, summary, `<metadata query="PostgreSQL database"`)
	assert.Contains(t, summary, `<token_budget mode=`)
	assert.Contains(t, summary, `<result_count>1</result_count>`)
	assert.Contains(t, summary, `<content>`, "XML content tag should be present")
	assert.Contains(t, summary, `</memory_context>`, "closing memory_context tag should be present")

	// Verify the content was properly escaped in XML
	assert.NotContains(t, summary, "<The user decided", "raw XML tags should be escaped")

	// Test with empty query
	emptyCtx, err := svc.RetrieveMemory(ctx, "user-bridge-1", "")
	require.NoError(t, err)
	assert.Empty(t, emptyCtx.RelatedEpisodes, "empty query should return empty context")

	// Test with non-matching query
	noMatchCtx, err := svc.RetrieveMemory(ctx, "user-bridge-1", "zzzzz unmatched query nnnnnnn")
	require.NoError(t, err)
	// May return empty or some results, but should not error
	t.Logf("Non-matching query returned %d episodes", len(noMatchCtx.RelatedEpisodes))
}

// ---------------------------------------------------------------------------
// Test: Dreamer in dry-run mode
// ---------------------------------------------------------------------------

func TestMemoryV2Integration_DreamerDryRun(t *testing.T) {
	repo := newMockIntegrationRepo()
	ctx := context.Background()

	// Save a memory for the dreamer to act upon.
	// Set ID explicitly so the mock response can reference it.
	memory := &types.AgentMemory{
		ID:          "dreamer-target-1",
		TenantID:    "tenant-dreamer-1",
		Content:     "The team decided to adopt Kubernetes for container orchestration across all environments.",
		MemoryType:  "decision",
		Verdict:     types.VerdictNone,
		Importance:  4,
	}
	err := repo.Create(ctx, memory)
	require.NoError(t, err)

	// Create a service with dry-run enabled
	config := types.DefaultMemoryV2Config()
	config.Dreamer.DryRun = true
	config.Dreamer.Enabled = true
	config.Dreamer.MaxActions = 5

	// Build a mock LLM response that proposes actions
	mockResponse := `{
		"actions": [
			{
				"type": "update_verdict",
				"target_id": "dreamer-target-1",
				"new_verdict": "fixed",
				"reason": "This is a confirmed decision",
				"confidence": 0.95
			},
			{
				"type": "adjust_importance",
				"target_id": "dreamer-target-1",
				"delta": 2,
				"reason": "Important architectural decision",
				"confidence": 0.85
			}
		]
	}`

	svc := &MemoryServiceV2Impl{
		repo:        repo,
		embedder:    &mockEmbedder{},
		config:      config,
		tokenBudget: NewTokenBudgetManager(),
	}

	// Wire up the dreamer with a mock chat
	svc.dreamer = workers.NewDreamerWorker(repo, &mockIntegrationChat{response: mockResponse}, config.Dreamer)

	// Execute the dreamer pass via ConsolidateDream
	result, err := svc.ConsolidateDream(ctx, "tenant-dreamer-1")
	require.NoError(t, err, "ConsolidateDream should succeed")
	require.NotNil(t, result, "DreamResult should not be nil")

	// In dry-run mode, actions are proposed and counted as applied but NOT actually persisted
	assert.Equal(t, 2, result.ActionsProposed, "both actions should be proposed")
	assert.Equal(t, 2, result.ActionsApplied, "both actions should be counted as applied in dry-run mode")

	// Verify actions were NOT actually applied to the stored memory
	stored, err := repo.GetByID(ctx, "tenant-dreamer-1", "dreamer-target-1")
	require.NoError(t, err)
	require.NotNil(t, stored)

	// The memory should still have its original verdict and importance since dry-run skips apply
	assert.Equal(t, types.VerdictNone, stored.Verdict, "verdict should not change in dry-run mode")
	assert.Equal(t, 4, stored.Importance, "importance should not be adjusted in dry-run mode")
}

// ---------------------------------------------------------------------------
// Test: Health check on empty KB
// ---------------------------------------------------------------------------

func TestMemoryV2Integration_HealthCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("empty knowledge base", func(t *testing.T) {
		repo := newMockIntegrationRepo()
		svc := newIntegrationService(repo)

		// Health check on an empty KB should return no critical issues
		issues, err := svc.AssessHealth(ctx, "tenant-health-empty-1", "")
		require.NoError(t, err, "AssessHealth should not error on empty KB")
		// Empty KB returns nil issues (health checker returns nil when no memories found)
		if issues != nil {
			assert.Empty(t, issues, "empty KB should have no health issues")
		}
	})

	t.Run("knowledge base with healthy memories", func(t *testing.T) {
		repo := newMockIntegrationRepo()
		// Add a well-formed memory with tags (simulating a healthy KB)
		healthyMem := &types.AgentMemory{
			ID:         "healthy-1",
			TenantID:   "tenant-health-full-1",
			Content:    "The user prefers clean architecture with dependency injection patterns throughout all services.",
			MemoryType: "preference",
			Importance: 4,
			Tier:       1,
			Tags:       []string{"architecture", "clean-code", "dependency-injection"},
			HubScore:   2.5,
			Verdict:    types.VerdictNone,
		}
		err := repo.Create(ctx, healthyMem)
		require.NoError(t, err)

		svc := newIntegrationService(repo)
		issues, err := svc.AssessHealth(ctx, "tenant-health-full-1", "")
		require.NoError(t, err)

		// A healthy memory with tags and hub score should not trigger orphan issues
		if issues != nil {
			for _, issue := range issues {
				assert.NotEqual(t, "orphan", issue.Type, "healthy memory should not be marked as orphan")
				assert.NotEqual(t, "critical", issue.Severity, "healthy KB should have no critical issues")
			}
		}
	})

	t.Run("knowledge base with orphan memories", func(t *testing.T) {
		repo := newMockIntegrationRepo()
		// Add an orphan memory (no tags, no hub score)
		orphanMem := &types.AgentMemory{
			ID:         "orphan-1",
			TenantID:   "tenant-health-orphan-1",
			Content:    "This is an isolated memory with no connections to other memories.",
			MemoryType: "semantic",
			Importance: 0,
			Tier:       2,
			Tags:       nil,
			HubScore:   0,
			Verdict:    types.VerdictNone,
		}
		err := repo.Create(ctx, orphanMem)
		require.NoError(t, err)

		svc := newIntegrationService(repo)
		issues, err := svc.AssessHealth(ctx, "tenant-health-orphan-1", "")
		require.NoError(t, err)
		require.NotNil(t, issues)

		// Should detect at least orphan issues or graph fragmentation
		foundOrphanIssue := false
		for _, issue := range issues {
			if issue.Type == "orphan" && issue.MemoryID == orphanMem.ID {
				foundOrphanIssue = true
				break
			}
		}
		assert.True(t, foundOrphanIssue, "should detect orphan memory")
	})

	t.Run("no critical issues on small healthy KB", func(t *testing.T) {
		repo := newMockIntegrationRepo()

		// Add some well-formed memories
		for i := 0; i < 5; i++ {
			mem := &types.AgentMemory{
				ID:         fmt.Sprintf("health-mem-%d", i),
				TenantID:   "tenant-health-crit-1",
				Content:    "This is healthy memory number " + fmt.Sprintf("%d", i+1) + " with tags and hub score.",
				MemoryType: "semantic",
				Importance: 3,
				Tier:       2,
				Tags:       []string{"test", "memory"},
				HubScore:   1.0,
				Verdict:    types.VerdictNone,
			}
			_ = repo.Create(ctx, mem)
		}

		svc := newIntegrationService(repo)
		issues, err := svc.AssessHealth(ctx, "tenant-health-crit-1", "")
		require.NoError(t, err)

		// Health report should not contain critical issues for a well-formed KB
		if issues != nil {
			for _, issue := range issues {
				assert.NotEqual(t, "critical", issue.Severity, "healthy KB should have no critical issues")
			}
			t.Logf("Found %d non-critical issues for healthy KB", len(issues))
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Verdict filtering
// ---------------------------------------------------------------------------

func TestMemoryV2Integration_VerdictFiltering(t *testing.T) {
	repo := newMockIntegrationRepo()
	svc := newIntegrationService(repo)
	ctx := context.Background()
	tenantID := "tenant-verdict-1"

	// Save memories with different verdicts
	memories := []*types.AgentMemory{
		{
			ID:         "verdict-none-1",
			TenantID:   tenantID,
			Content:    "The user mentioned they enjoy hiking on weekends.",
			MemoryType: "preference",
			Verdict:    types.VerdictNone,
			Importance: 2,
			Tier:       2,
			CreatedAt:  time.Now(),
		},
		{
			ID:         "verdict-refuted-1",
			TenantID:   tenantID,
			Content:    "The user said they dislike outdoor activities.",
			MemoryType: "preference",
			Verdict:    types.VerdictRefuted,
			Importance: 1,
			Tier:       2,
			CreatedAt:  time.Now(),
		},
		{
			ID:         "verdict-decision-1",
			TenantID:   tenantID,
			Content:    "The team decided to use Go for all backend services.",
			MemoryType: "decision",
			Verdict:    types.VerdictDecision,
			Importance: 5,
			Tier:       1,
			CreatedAt:  time.Now(),
		},
		{
			ID:         "verdict-fixed-1",
			TenantID:   tenantID,
			Content:    "The API endpoint /api/v1/users returns paginated results.",
			MemoryType: "semantic",
			Verdict:    types.VerdictFixed,
			Importance: 3,
			Tier:       2,
			CreatedAt:  time.Now(),
		},
	}

	for _, mem := range memories {
		err := repo.Create(ctx, mem)
		require.NoError(t, err, "should create memory %s", mem.ID)
	}

	t.Run("default search excludes refuted", func(t *testing.T) {
		results, err := svc.SearchMemories(ctx, "user", &types.MemoryFilter{
			TenantID: tenantID,
			Limit:    10,
		})
		require.NoError(t, err)

		// Refuted should be excluded by default
		for _, r := range results {
			assert.NotEqual(t, types.VerdictRefuted, r.Memory.Verdict,
				"refuted memory should be excluded by default")
		}

		// The non-refuted memories should be present
		ids := make(map[string]bool)
		for _, r := range results {
			ids[r.Memory.ID] = true
		}
		assert.True(t, ids["verdict-none-1"], "none verdict should be included")
		assert.True(t, ids["verdict-decision-1"], "decision verdict should be included")
		assert.True(t, ids["verdict-fixed-1"], "fixed verdict should be included")
		assert.False(t, ids["verdict-refuted-1"], "refuted verdict should be excluded")
	})

	t.Run("explicit refuted filter includes refuted", func(t *testing.T) {
		results, err := svc.SearchMemories(ctx, "outdoor activities", &types.MemoryFilter{
			TenantID: tenantID,
			Verdicts: []types.MemoryVerdict{types.VerdictRefuted},
			Limit:    10,
		})
		require.NoError(t, err)

		// The refuted memory should now be included
		foundRefuted := false
		for _, r := range results {
			if r.Memory.ID == "verdict-refuted-1" {
				foundRefuted = true
				break
			}
		}
		assert.True(t, foundRefuted, "refuted memory should be included when explicitly requested")
	})

	t.Run("search with multiple verdicts", func(t *testing.T) {
		results, err := svc.SearchMemories(ctx, "user", &types.MemoryFilter{
			TenantID: tenantID,
			Verdicts: []types.MemoryVerdict{types.VerdictDecision, types.VerdictFixed},
			Limit:    10,
		})
		require.NoError(t, err)

		ids := make(map[string]bool)
		for _, r := range results {
			ids[r.Memory.ID] = true
		}
		assert.True(t, ids["verdict-decision-1"], "decision should be included")
		assert.True(t, ids["verdict-fixed-1"], "fixed should be included")
		assert.False(t, ids["verdict-none-1"], "none verdict should be excluded when verdicts are explicitly specified")
	})

	t.Run("search with all verdicts includes everything", func(t *testing.T) {
		results, err := svc.SearchMemories(ctx, "user", &types.MemoryFilter{
			TenantID: tenantID,
			Verdicts: []types.MemoryVerdict{
				types.VerdictNone,
				types.VerdictRefuted,
				types.VerdictDecision,
				types.VerdictFixed,
			},
			Limit: 10,
		})
		require.NoError(t, err)

		ids := make(map[string]bool)
		for _, r := range results {
			ids[r.Memory.ID] = true
		}
		assert.True(t, ids["verdict-none-1"], "none should be included")
		assert.True(t, ids["verdict-refuted-1"], "refuted should be included")
		assert.True(t, ids["verdict-decision-1"], "decision should be included")
		assert.True(t, ids["verdict-fixed-1"], "fixed should be included")
	})
}

// ---------------------------------------------------------------------------
// Mock types used across integration tests
// ---------------------------------------------------------------------------
