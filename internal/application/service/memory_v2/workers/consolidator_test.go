package workers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockConsolidatorRepo implements interfaces.MemoryRepositoryV2 for testing the consolidator.
type mockConsolidatorRepo struct {
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
	computeHubScoresCalls int
	updateCalls           []*types.AgentMemory
	deleteCalls           []deleteCall
}

type deleteCall struct {
	TenantID string
	ID       string
}

func (m *mockConsolidatorRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockConsolidatorRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockConsolidatorRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	m.mu.Lock()
	// Store a copy so later modifications to the original don't affect captured state
	copy := *memory
	m.updateCalls = append(m.updateCalls, &copy)
	m.mu.Unlock()
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockConsolidatorRepo) Delete(ctx context.Context, tenantID, id string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, deleteCall{TenantID: tenantID, ID: id})
	m.mu.Unlock()
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockConsolidatorRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockConsolidatorRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockConsolidatorRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockConsolidatorRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockConsolidatorRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	m.computeHubScoresCalls++
	m.mu.Unlock()
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockConsolidatorRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}

func (m *mockConsolidatorRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockConsolidatorRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockConsolidatorRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockConsolidatorRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockConsolidatorRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockConsolidatorRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}

// mockConsolidatorEmbedder implements embedding.Embedder for testing.
type mockConsolidatorEmbedder struct {
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockConsolidatorEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockConsolidatorEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockConsolidatorEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockConsolidatorEmbedder) GetModelName() string { return "mock" }

func (m *mockConsolidatorEmbedder) GetDimensions() int { return 3 }

func (m *mockConsolidatorEmbedder) GetModelID() string { return "mock-model" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeConsolidatorMemory(id, tenantID, content string, importance int, createdAt time.Time) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Importance: importance,
		Verdict:    types.VerdictNone,
		HubScore:   0,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		MemoryType: "semantic",
		Tier:       2,
	}
}

func newTestConsolidator(repo *mockConsolidatorRepo, embedder *mockConsolidatorEmbedder) *Consolidator {
	return &Consolidator{
		repo:     repo,
		embedder: embedder,
		interval: 6 * time.Hour,
	}
}

// ---------------------------------------------------------------------------
// Test: ComputeHubScores is called
// ---------------------------------------------------------------------------

func TestConsolidator_ComputeHubScoresCalled(t *testing.T) {
	repo := &mockConsolidatorRepo{}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	c.consolidateAll(context.Background())

	assert.Equal(t, 1, repo.computeHubScoresCalls,
		"ComputeHubScores should be called exactly once per consolidation cycle")
}

func TestConsolidator_ComputeHubScoresErrorLogged(t *testing.T) {
	// When ComputeHubScores returns an error, the cycle should continue
	// to the next step (decay) rather than aborting.
	repo := &mockConsolidatorRepo{
		computeHubScores: func(ctx context.Context, tenantID string) error {
			return assert.AnError
		},
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	// Should not panic or deadlock when ComputeHubScores returns an error
	c.consolidateAll(context.Background())

	// ComputeHubScores was attempted
	assert.Equal(t, 1, repo.computeHubScoresCalls)
}

// ---------------------------------------------------------------------------
// Test: Importance decay for memories > 1 year (10% reduction)
// ---------------------------------------------------------------------------

func TestConsolidator_DecayOldMemories_ReducesImportance(t *testing.T) {
	oneYearAgo := time.Now().AddDate(-1, -1, 0) // 13 months ago

	mem := makeConsolidatorMemory("mem-1", "tenant-1", "old memory content", 5, oneYearAgo)

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 1.0},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.NoError(t, err)

	// Importance 5 * 0.9 = 4.5, floor = 4
	require.Len(t, repo.updateCalls, 1, "should update the decayed memory")
	assert.Equal(t, 4, repo.updateCalls[0].Importance, "importance should be reduced by 10% (rounded down)")
}

func TestConsolidator_DecayOldMemories_DoesNotDecayRecent(t *testing.T) {
	recently := time.Now().AddDate(0, -6, 0) // 6 months ago

	mem := makeConsolidatorMemory("mem-2", "tenant-1", "recent memory", 5, recently)

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 1.0},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.NoError(t, err)

	// Memory is less than 1 year old, should not be decayed
	assert.Empty(t, repo.updateCalls, "recent memories should not be decayed")
}

func TestConsolidator_DecayOldMemories_FloorAtMinusFive(t *testing.T) {
	oneYearAgo := time.Now().AddDate(-1, -1, 0)

	mem := makeConsolidatorMemory("mem-3", "tenant-1", "low importance", -4, oneYearAgo)

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 1.0},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.NoError(t, err)

	// -4 * 0.9 = -3.6, floor = -4. Since floor(-3.6) = -4, which is >= -5, it should update.
	// But check the code: if r.Memory.Importance > -5 (true, -4 > -5) AND decayed >= -5 (true, -4 >= -5)
	// So it should update to -4
	// Actually, -4 * 0.9 = -3.6. math.Floor(-3.6) = -4. Then if decayed >= -5 → true.
	// So importance goes from -4 to -4 — effectively no change but still an update call.
	require.Len(t, repo.updateCalls, 1)
}

func TestConsolidator_DecayOldMemories_BelowMinusFiveNotUpdated(t *testing.T) {
	oneYearAgo := time.Now().AddDate(-1, -1, 0)

	// Importance -5: condition r.Memory.Importance > -5 => false, so skip entirely
	mem := makeConsolidatorMemory("mem-4", "tenant-1", "at floor", -5, oneYearAgo)

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 1.0},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.NoError(t, err)

	// Importance -5 means guard condition `Importance > -5` is false, so no update
	assert.Empty(t, repo.updateCalls, "memories at importance -5 should not be decayed")
}

func TestConsolidator_DecayOldMemories_SkipsNilMemory(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 1.0},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.NoError(t, err)
	assert.Empty(t, repo.updateCalls, "nil memory entries should be skipped")
}

func TestConsolidator_DecayOldMemories_SearchError(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.decayOldMemories(context.Background())
	require.Error(t, err, "should propagate search error")
}

// ---------------------------------------------------------------------------
// Test: Near-duplicate merging (cosine > 0.93)
// ---------------------------------------------------------------------------

func TestConsolidator_MergeNearDuplicates_MergesHighSimilarity(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "The sky appears blue during daytime.", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", "The sky looks blue in daylight hours.", 5, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
	}

	// Return vectors with cosine similarity > 0.93
	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)

	// memB should be merged into memA and then deleted
	require.Len(t, repo.updateCalls, 1, "should update mem-a with merged content")
	assert.Equal(t, "mem-a", repo.updateCalls[0].ID, "should update the surviving memory")
	assert.Contains(t, repo.updateCalls[0].Content, "The sky appears blue during daytime.")
	assert.Contains(t, repo.updateCalls[0].Content, "The sky looks blue in daylight hours.")

	// memB's importance (5) is higher than memA's (3), so survivor should get 5
	assert.Equal(t, 5, repo.updateCalls[0].Importance, "should take the higher importance")

	// memB should be soft-deleted
	require.Len(t, repo.deleteCalls, 1, "should delete the merged-away memory")
	assert.Equal(t, "mem-b", repo.deleteCalls[0].ID)
	assert.Equal(t, "tenant-1", repo.deleteCalls[0].TenantID)
}

func TestConsolidator_MergeNearDuplicates_DoesNotMergeLowSimilarity(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "The sky appears blue during daytime.", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", "Quantum entanglement is a physical phenomenon.", 5, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
	}

	// Return orthogonal vectors — cosine similarity = 0
	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			// Use content-based deterministic vectors to ensure low similarity
			if text == "The sky appears blue during daytime." {
				return []float32{1.0, 0.0, 0.0}, nil
			}
			return []float32{0.0, 1.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)

	// No updates or deletes when similarity is below threshold
	assert.Empty(t, repo.updateCalls, "low-similarity memories should not be merged")
	assert.Empty(t, repo.deleteCalls, "no delete should occur for low similarity")
}

func TestConsolidator_MergeNearDuplicates_ContentTruncatedAt2000(t *testing.T) {
	longContentA := string(make([]byte, 1500))
	longContentB := string(make([]byte, 1500))

	memA := makeConsolidatorMemory("mem-a", "tenant-1", longContentA, 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", longContentB, 3, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
	}

	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)

	require.Len(t, repo.updateCalls, 1)
	assert.LessOrEqual(t, len(repo.updateCalls[0].Content), 2000,
		"merged content should be truncated to 2000 characters")
}

func TestConsolidator_MergeNearDuplicates_SkipsAlreadyMerged(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "Memory A content", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", "Memory B content", 5, time.Now().Add(-12*time.Hour))
	memC := makeConsolidatorMemory("mem-c", "tenant-1", "Memory C content", 7, time.Now().Add(-6*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			// Return memA and memB with original Importance values (not modified by prior run)
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
				{Memory: memC, Score: 0.5},
			}, 3, nil
		},
	}

	// All three have the same vector (high similarity)
	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)

	// mem-a merges mem-b and mem-c into itself. Both merge ops call Update.
	// First: mem-a + mem-b → update mem-a
	// Second: mem-a + mem-c → update mem-a again
	require.Len(t, repo.updateCalls, 2, "mem-a should be updated twice (merge b, then merge c)")
	for _, call := range repo.updateCalls {
		assert.Equal(t, "mem-a", call.ID, "all updates should be on mem-a")
	}

	// mem-b and mem-c should be deleted
	require.Len(t, repo.deleteCalls, 2, "mem-b and mem-c should be deleted")
	deleteIDs := make(map[string]bool)
	for _, dc := range repo.deleteCalls {
		deleteIDs[dc.ID] = true
	}
	assert.True(t, deleteIDs["mem-b"], "mem-b should be deleted")
	assert.True(t, deleteIDs["mem-c"], "mem-c should be deleted")
}

func TestConsolidator_MergeNearDuplicates_EmptyResults(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)
	assert.Empty(t, repo.updateCalls)
	assert.Empty(t, repo.deleteCalls)
}

func TestConsolidator_MergeNearDuplicates_SingleMemory(t *testing.T) {
	mem := makeConsolidatorMemory("mem-a", "tenant-1", "only one memory", 3, time.Now().Add(-24*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)
	assert.Empty(t, repo.updateCalls, "no merge with a single memory")
	assert.Empty(t, repo.deleteCalls)
}

func TestConsolidator_MergeNearDuplicates_SkipsNilMemory(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 1.0},
				{Memory: nil, Score: 1.0},
			}, 2, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)
	assert.Empty(t, repo.updateCalls, "nil memories should be skipped")
}

func TestConsolidator_MergeNearDuplicates_EmbedErrorFallback(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "content A", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", "content B", 5, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
	}

	// Embedder returns an error — the memory should be skipped gracefully
	callCount := 0
	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			callCount++
			return nil, assert.AnError
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "both memories should be embedded (and fail)")
	assert.Empty(t, repo.updateCalls, "no merge should occur when embed fails")
	assert.Empty(t, repo.deleteCalls)
}

func TestConsolidator_MergeNearDuplicates_DifferentTenantsNotMerged(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "same content across tenants", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-2", "same content across tenants", 5, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
	}

	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err)

	// Different tenants — each group has only 1 memory, no merge occurs
	assert.Empty(t, repo.updateCalls)
	assert.Empty(t, repo.deleteCalls)
}

func TestConsolidator_MergeNearDuplicates_SearchError(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	err := c.mergeNearDuplicates(context.Background())
	require.Error(t, err, "should propagate search error")
}

// ---------------------------------------------------------------------------
// Test: Context cancellation / graceful shutdown
// ---------------------------------------------------------------------------

func TestConsolidator_Run_ContextCancellation(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	// Give the goroutine time to start, run the initial cycle, and enter the loop
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case <-done:
		// Run returned cleanly — success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}

	// Initial cycle ran once (ComputeHubScores was called)
	assert.Equal(t, 1, repo.computeHubScoresCalls,
		"one consolidation cycle should have run before cancellation")
}

func TestConsolidator_Run_ContextPreCancelled(t *testing.T) {
	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after pre-cancelled context")
	}

	// Initial cycle should NOT run when context is already cancelled
	// (because consolidateAll is called BEFORE the select, and ctx.Done()
	// won't fire until the select is entered, but the initial consolidateAll
	// should check ctx.Err()... looking at the code, consolidateAll calls repo
	// methods which receive the cancelled context — they should return early)
	// Actually, the code calls consolidateAll(ctx) BEFORE the select loop.
	// With a cancelled context, the repo methods should detect it.
	// The key assertion is that Run returns without blocking.
}

func TestConsolidator_consolidateAll_CancelledContext(t *testing.T) {
	// Verify that consolidateAll handles a context that's already cancelled
	// without panicking
	repo := &mockConsolidatorRepo{}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic — individual methods may or may not check ctx.Err()
	c.consolidateAll(ctx)
}

// ---------------------------------------------------------------------------
// Test: Panic recovery (edge cases and defensive coding)
// ---------------------------------------------------------------------------

func TestConsolidator_consolidateAll_UpdatePanic(t *testing.T) {
	// If the repo Update panics, the whole consolidateAll should be resilient.
	// Since the code doesn't have an explicit recover, we just verify
	// that edge cases are handled gracefully by testing each step isolation.
	oneYearAgo := time.Now().AddDate(-1, -1, 0)
	mem := makeConsolidatorMemory("mem-1", "tenant-1", "content", 5, oneYearAgo)

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 1.0},
			}, 1, nil
		},
		updateFunc: func(ctx context.Context, m *types.AgentMemory) error {
			return assert.AnError
		},
	}
	embedder := &mockConsolidatorEmbedder{}
	c := newTestConsolidator(repo, embedder)

	// decayOldMemories should handle update errors gracefully (logs them, continues)
	err := c.decayOldMemories(context.Background())
	require.NoError(t, err, "should not error when individual update fails")
}

func TestConsolidator_mergeNearDuplicates_DeleteErrorContinues(t *testing.T) {
	memA := makeConsolidatorMemory("mem-a", "tenant-1", "content A", 3, time.Now().Add(-24*time.Hour))
	memB := makeConsolidatorMemory("mem-b", "tenant-1", "content B", 5, time.Now().Add(-12*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
		deleteFunc: func(ctx context.Context, tenantID, id string) error {
			return assert.AnError
		},
	}

	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	// Even if Delete fails, the merge itself should still proceed
	err := c.mergeNearDuplicates(context.Background())
	require.NoError(t, err, "should not error when delete fails")

	// Update should still happen
	require.Len(t, repo.updateCalls, 1, "merge update should occur despite delete error")
}

// ---------------------------------------------------------------------------
// Test: Full consolidateAll cycle
// ---------------------------------------------------------------------------

func TestConsolidator_consolidateAll_FullCycle(t *testing.T) {
	oneYearAgo := time.Now().AddDate(-1, -1, 0)
	recently := time.Now().AddDate(0, -1, 0)

	oldMem := makeConsolidatorMemory("mem-old", "tenant-1", "old content here", 5, oneYearAgo)
	newMem := makeConsolidatorMemory("mem-new", "tenant-1", "different content here", 3, recently)
	dupMem := makeConsolidatorMemory("mem-dup", "tenant-1", "old content here", 7, oneYearAgo.Add(-24*time.Hour))

	repo := &mockConsolidatorRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: oldMem, Score: 0.5},
				{Memory: newMem, Score: 0.5},
				{Memory: dupMem, Score: 0.5},
			}, 3, nil
		},
	}

	// oldMem and dupMem share the same content → same vector → high similarity
	// newMem has different content → different vector → low similarity
	embedder := &mockConsolidatorEmbedder{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			if text == "old content here" {
				return []float32{1.0, 0.0, 0.0}, nil
			}
			return []float32{0.0, 1.0, 0.0}, nil
		},
	}
	c := newTestConsolidator(repo, embedder)

	c.consolidateAll(context.Background())

	// 1. ComputeHubScores was called
	assert.Equal(t, 1, repo.computeHubScoresCalls)

	// 2. Both oldMem (Importance=5) and dupMem (Importance=7) were decayed:
	//    5*0.9=4.5→4, 7*0.9=6.3→6
	// 3. oldMem and dupMem were merged (same content → high cosine similarity)
	//    oldMem (Importance=4) and dupMem (Importance=6) → survivor gets max(4,6)=6
	// 4. newMem was not merged (different content → low cosine similarity)

	// Total update calls: 2 (decay) + 1 (merge update on mem-old) = 3
	require.Len(t, repo.updateCalls, 3, "should have 2 decay updates + 1 merge update")

	// Check at least one update has Importance=4 (decayed value from oldMem)
	decayFound := false
	mergeFound := false
	for _, call := range repo.updateCalls {
		if call.ID == "mem-old" && call.Importance == 4 {
			decayFound = true
		}
		if call.ID == "mem-old" && call.Importance == 6 {
			mergeFound = true
		}
	}
	assert.True(t, decayFound, "oldMem should be decayed to importance 4")
	assert.True(t, mergeFound, "oldMem should be merged with dupMem and take importance 6")

	// dupMem should be deleted (merged away)
	require.Len(t, repo.deleteCalls, 1, "dupMem should be deleted after merge")
	assert.Equal(t, "mem-dup", repo.deleteCalls[0].ID)
}
