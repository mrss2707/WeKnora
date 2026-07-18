package workers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockCacheWarmerRepo implements interfaces.MemoryRepositoryV2 for testing the cache warmer.
type mockCacheWarmerRepo struct {
	mu                 sync.Mutex
	createFunc         func(ctx context.Context, memory *types.AgentMemory) error
	getByIDFunc        func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error)
	updateFunc         func(ctx context.Context, memory *types.AgentMemory) error
	deleteFunc         func(ctx context.Context, tenantID, id string) error
	searchFunc         func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error)
	cosineSearchFunc   func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)
	tryDreamerLockFunc func(ctx context.Context, tenantID string, workerID string) (bool, error)
	unlockDreamerFunc  func(ctx context.Context, tenantID string) error
	computeHubScores   func(ctx context.Context, tenantID string) error
	invalidateCache    func(ctx context.Context, tenantID string)

	// Tracking
	searchCalls int
}

func (m *mockCacheWarmerRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockCacheWarmerRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockCacheWarmerRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockCacheWarmerRepo) Delete(ctx context.Context, tenantID, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockCacheWarmerRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	m.mu.Lock()
	m.searchCalls++
	m.mu.Unlock()
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockCacheWarmerRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockCacheWarmerRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockCacheWarmerRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockCacheWarmerRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockCacheWarmerRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}
func (m *mockCacheWarmerRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockCacheWarmerRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockCacheWarmerRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockCacheWarmerRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockCacheWarmerRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockCacheWarmerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}

// mockCacheWarmerEmbedder implements embedding.Embedder for testing the cache warmer.
type mockCacheWarmerEmbedder struct {
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockCacheWarmerEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockCacheWarmerEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockCacheWarmerEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockCacheWarmerEmbedder) GetModelName() string { return "mock" }

func (m *mockCacheWarmerEmbedder) GetDimensions() int { return 3 }

func (m *mockCacheWarmerEmbedder) GetModelID() string { return "mock-model" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeCacheWarmerMemory(id, tenantID, content string) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Importance: 3,
		Verdict:    types.VerdictNone,
		HubScore:   0,
		MemoryType: "semantic",
		Tier:       2,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func newTestCacheWarmer(repo *mockCacheWarmerRepo, embedder *mockCacheWarmerEmbedder, enabled bool, topN int, interval time.Duration) *CacheWarmer {
	config := types.CacheWarmerConfig{
		Enabled:         enabled,
		TopQueriesN:     topN,
		RefreshInterval: interval.String(),
	}
	return &CacheWarmer{
		repo:     repo,
		embedder: embedder,
		config:   config,
		interval: interval,
	}
}

// ---------------------------------------------------------------------------
// Test: warmCache warms top N queries on startup
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_WarmsTopN(t *testing.T) {
	memories := []*types.MemorySearchResult{
		{Memory: makeCacheWarmerMemory("mem-1", "tenant-1", "content one"), Score: 0.9},
		{Memory: makeCacheWarmerMemory("mem-2", "tenant-1", "content two"), Score: 0.8},
	}

	embedCalls := 0
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return memories, 2, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be called once")
	assert.Equal(t, 2, embedCalls, "each memory with content should be embedded")
}

func TestCacheWarmer_warmCache_UsesConfiguredTopN(t *testing.T) {
	memories := make([]*types.MemorySearchResult, 50)
	for i := 0; i < 50; i++ {
		memories[i] = &types.MemorySearchResult{
			Memory: makeCacheWarmerMemory(
				fmt.Sprintf("mem-%d", i), "tenant-1", fmt.Sprintf("content %d", i)),
			Score: 1.0,
		}
	}

	embedCalls := 0
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 10, 30*time.Minute)

	// With topN=10 but search returns 50, limit should be 10
	repo.searchFunc = func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
		// The filter should have Limit=10
		assert.Equal(t, 10, filter.Limit, "filter limit should be set to configured TopQueriesN")
		return memories[:10], 10, nil
	}

	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls)
	assert.Equal(t, 10, embedCalls, "should embed exactly the top 10 memories")
}

func TestCacheWarmer_warmCache_DefaultTopIIs100(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			assert.Equal(t, 100, filter.Limit, "default TopQueriesN should be 100")
			return nil, 0, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{}
	cw := &CacheWarmer{
		repo:     repo,
		embedder: embedder,
		config: types.CacheWarmerConfig{
			Enabled:         true,
			TopQueriesN:     0, // zero value, triggers default
			RefreshInterval: "30m",
		},
		interval: 30 * time.Minute,
	}

	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be called")
}

// ---------------------------------------------------------------------------
// Test: Periodic refresh every 30m
// ---------------------------------------------------------------------------

func TestCacheWarmer_Run_PeriodicRefresh(t *testing.T) {
	warmCount := 0
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			warmCount++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cw.Run(ctx)
		close(done)
	}()

	// Allow time for initial warm + at least one periodic refresh
	time.Sleep(120 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}

	// Should have: 1 initial warm + at least 1 periodic refresh
	assert.GreaterOrEqual(t, repo.searchCalls, 2, "search should run at least twice (initial + periodic)")
}

// ---------------------------------------------------------------------------
// Test: Disabled by config (no warming when Enabled=false)
// ---------------------------------------------------------------------------

func TestCacheWarmer_Run_DisabledByConfig(t *testing.T) {
	repo := &mockCacheWarmerRepo{}
	embedder := &mockCacheWarmerEmbedder{}
	cw := newTestCacheWarmer(repo, embedder, false, 100, 30*time.Minute)

	ctx := context.Background()
	cw.Run(ctx)

	// Should return immediately without calling search or embed
	assert.Equal(t, 0, repo.searchCalls, "no search should occur when disabled")
}

// ---------------------------------------------------------------------------
// Test: Context cancellation
// ---------------------------------------------------------------------------

func TestCacheWarmer_Run_ContextCancellation(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cw.Run(ctx)
		close(done)
	}()

	// Give the goroutine time to start, run the initial warm, and enter the loop
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}

	// Initial warm should have run before cancellation
	assert.Equal(t, 1, repo.searchCalls, "initial warm should run before cancellation")
}

func TestCacheWarmer_Run_PreCancelledContext(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockCacheWarmerEmbedder{}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		cw.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after pre-cancelled context")
	}

	// Pre-cancelled context: Run calls warmCache which calls search.
	// The cancelled context may or may not propagate into search depending
	// on implementation. The key assertion is Run returns without blocking.
}

// ---------------------------------------------------------------------------
// Test: Empty result set (no memories to warm)
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_EmptyResults(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedCalls := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be called")
	assert.Equal(t, 0, embedCalls, "no embeddings should be computed with empty results")
}

// ---------------------------------------------------------------------------
// Test: Nil memory in results is skipped
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_SkipsNilMemory(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 1.0},
				{Memory: nil, Score: 1.0},
			}, 2, nil
		},
	}
	embedCalls := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	cw.warmCache(context.Background())

	assert.Equal(t, 0, embedCalls, "nil memories should be skipped")
}

// ---------------------------------------------------------------------------
// Test: Memory with empty content is skipped
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_SkipsEmptyContent(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: makeCacheWarmerMemory("mem-1", "tenant-1", ""), Score: 1.0},
				{Memory: makeCacheWarmerMemory("mem-2", "tenant-1", "real content"), Score: 1.0},
			}, 2, nil
		},
	}
	embedCalls := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	cw.warmCache(context.Background())

	assert.Equal(t, 1, embedCalls, "only memory with non-empty content should be embedded")
}

// ---------------------------------------------------------------------------
// Test: Search error handled gracefully
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_SearchError(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	embedCalls := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	// Should not panic — logs the error and returns
	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be attempted")
	assert.Equal(t, 0, embedCalls, "no embeddings should be computed after search error")
}

// ---------------------------------------------------------------------------
// Test: Embedding error handled gracefully (continues to next memory)
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_EmbedErrorContinues(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: makeCacheWarmerMemory("mem-1", "tenant-1", "first content"), Score: 1.0},
				{Memory: makeCacheWarmerMemory("mem-2", "tenant-1", "second content"), Score: 1.0},
			}, 2, nil
		},
	}
	embedCallCount := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCallCount++
			if embedCallCount == 1 {
				return nil, assert.AnError
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	// Should not panic when first embed fails — should continue to second
	cw.warmCache(context.Background())

	assert.Equal(t, 2, embedCallCount, "both memories should be attempted even if first fails")
}

// ---------------------------------------------------------------------------
// Test: warmCache with mixed memories (some nil, some empty, some valid)
// ---------------------------------------------------------------------------

func TestCacheWarmer_warmCache_MixedMemories(t *testing.T) {
	repo := &mockCacheWarmerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 1.0},
				{Memory: makeCacheWarmerMemory("mem-1", "tenant-1", "valid content"), Score: 0.9},
				{Memory: makeCacheWarmerMemory("mem-2", "tenant-1", ""), Score: 0.8},
				{Memory: makeCacheWarmerMemory("mem-3", "tenant-1", "more valid"), Score: 0.7},
			}, 4, nil
		},
	}
	embedCalls := 0
	embedder := &mockCacheWarmerEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCalls++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	cw := newTestCacheWarmer(repo, embedder, true, 100, 30*time.Minute)

	cw.warmCache(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be called once")
	assert.Equal(t, 2, embedCalls, "only the two valid-content memories should be embedded")
}
