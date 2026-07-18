package workers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockPrunerRepo implements interfaces.MemoryRepositoryV2 for testing the pruner.
type mockPrunerRepo struct {
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
	deleteCalls []deleteCall
	searchCalls int
}

func (m *mockPrunerRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockPrunerRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockPrunerRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockPrunerRepo) Delete(ctx context.Context, tenantID, id string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, deleteCall{TenantID: tenantID, ID: id})
	m.mu.Unlock()
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockPrunerRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	m.mu.Lock()
	m.searchCalls++
	m.mu.Unlock()
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockPrunerRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockPrunerRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockPrunerRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockPrunerRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockPrunerRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}

func (m *mockPrunerRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockPrunerRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockPrunerRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockPrunerRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockPrunerRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockPrunerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makePrunerMemory(id, tenantID, content string, tier int, tags []string, createdAt, updatedAt time.Time) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Importance: 3,
		Verdict:    types.VerdictNone,
		HubScore:   0,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		MemoryType: "semantic",
		Tier:       tier,
		Tags:       tags,
	}
}

func newTestPruner(repo *mockPrunerRepo) *Pruner {
	return &Pruner{repo: repo}
}

// ---------------------------------------------------------------------------
// Test: Tier-3 memories past TTL (7d) soft-deleted immediately
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_Tier3PastTTL(t *testing.T) {
	mem := makePrunerMemory("mem-1", "tenant-1", "tier-3 old", 3, nil,
		time.Now().Add(-10*24*time.Hour), // created 10 days ago (past 7d TTL)
		time.Now().Add(-10*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	require.Len(t, repo.deleteCalls, 1, "tier-3 past 7d TTL should be soft-deleted")
	assert.Equal(t, "mem-1", repo.deleteCalls[0].ID)
	assert.Equal(t, "tenant-1", repo.deleteCalls[0].TenantID)
}

func TestPruner_SoftDeleteExpired_Tier3BeforeTTL(t *testing.T) {
	mem := makePrunerMemory("mem-1", "tenant-1", "tier-3 recent", 3, nil,
		time.Now().Add(-3*24*time.Hour), // created 3 days ago (within 7d TTL)
		time.Now().Add(-3*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "tier-3 within TTL should not be deleted")
}

// ---------------------------------------------------------------------------
// Test: Tier-1/2 memories expired AND not accessed > 30d soft-deleted
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_Tier2ExpiredAndInactive(t *testing.T) {
	// Tier-2: 30 day TTL + 30 day inactivity
	mem := makePrunerMemory("mem-2", "tenant-1", "tier-2 old", 2, nil,
		time.Now().Add(-60*24*time.Hour), // created 60 days ago (past 30d TTL)
		time.Now().Add(-45*24*time.Hour), // updated 45 days ago (past 30d inactivity)
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	require.Len(t, repo.deleteCalls, 1, "tier-2 expired and inactive should be soft-deleted")
	assert.Equal(t, "mem-2", repo.deleteCalls[0].ID)
}

func TestPruner_SoftDeleteExpired_Tier1ExpiredAndInactive(t *testing.T) {
	// Tier-1: 90 day TTL + 30 day inactivity
	mem := makePrunerMemory("mem-1", "tenant-1", "tier-1 old", 1, nil,
		time.Now().Add(-120*24*time.Hour), // created 120 days ago (past 90d TTL)
		time.Now().Add(-45*24*time.Hour),  // updated 45 days ago (past 30d inactivity)
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	require.Len(t, repo.deleteCalls, 1, "tier-1 expired and inactive should be soft-deleted")
	assert.Equal(t, "mem-1", repo.deleteCalls[0].ID)
}

// ---------------------------------------------------------------------------
// Test: Tier-1/2 memories expired but accessed recently NOT deleted
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_Tier2ExpiredButActive(t *testing.T) {
	// Tier-2: TTL expired but accessed recently (<30d ago)
	mem := makePrunerMemory("mem-2", "tenant-1", "tier-2 active", 2, nil,
		time.Now().Add(-60*24*time.Hour), // created 60 days ago (past 30d TTL)
		time.Now().Add(-24*time.Hour),    // updated 1 day ago (recent)
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "tier-2 expired but recently accessed should not be deleted")
}

func TestPruner_SoftDeleteExpired_Tier1ExpiredButActive(t *testing.T) {
	// Tier-1: TTL expired but accessed recently (<30d ago)
	mem := makePrunerMemory("mem-1", "tenant-1", "tier-1 active", 1, nil,
		time.Now().Add(-120*24*time.Hour), // created 120 days ago (past 90d TTL)
		time.Now().Add(-24*time.Hour),     // updated 1 day ago (recent)
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "tier-1 expired but recently accessed should not be deleted")
}

// ---------------------------------------------------------------------------
// Test: Tier-0 (critical) NEVER deleted
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_Tier0NeverDeleted(t *testing.T) {
	mem := makePrunerMemory("mem-0", "tenant-1", "critical memory", 0, nil,
		time.Now().Add(-365*24*time.Hour), // created 1 year ago
		time.Now().Add(-365*24*time.Hour), // never accessed
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "tier-0 memories should never be deleted")
}

// ---------------------------------------------------------------------------
// Test: Memories tagged "critical" or "permanent" NEVER deleted
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_TaggedCriticalNeverDeleted(t *testing.T) {
	mem := makePrunerMemory("mem-tag", "tenant-1", "tagged critical", 3, []string{"critical"},
		time.Now().Add(-20*24*time.Hour),
		time.Now().Add(-20*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "memories tagged 'critical' should never be deleted")
}

func TestPruner_SoftDeleteExpired_TaggedPermanentNeverDeleted(t *testing.T) {
	mem := makePrunerMemory("mem-tag", "tenant-1", "tagged permanent", 3, []string{"permanent"},
		time.Now().Add(-20*24*time.Hour),
		time.Now().Add(-20*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "memories tagged 'permanent' should never be deleted")
}

func TestPruner_SoftDeleteExpired_TaggedCriticalCaseInsensitive(t *testing.T) {
	mem := makePrunerMemory("mem-tag", "tenant-1", "tagged CRITICAL", 3, []string{"CRITICAL"},
		time.Now().Add(-20*24*time.Hour),
		time.Now().Add(-20*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "case-insensitive 'CRITICAL' tag should also protect")
}

func TestPruner_SoftDeleteExpired_TaggedPermanentMixedCase(t *testing.T) {
	mem := makePrunerMemory("mem-tag", "tenant-1", "tagged Permanent", 3, []string{"Permanent"},
		time.Now().Add(-20*24*time.Hour),
		time.Now().Add(-20*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "case-insensitive 'Permanent' tag should also protect")
}

func TestPruner_SoftDeleteExpired_NonProtectedTagDoesNotProtect(t *testing.T) {
	mem := makePrunerMemory("mem-tag", "tenant-1", "tagged unimportant", 3, []string{"unimportant"},
		time.Now().Add(-20*24*time.Hour),
		time.Now().Add(-20*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: mem, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	require.Len(t, repo.deleteCalls, 1, "non-protected tag should not prevent deletion")
	assert.Equal(t, "mem-tag", repo.deleteCalls[0].ID)
}

// ---------------------------------------------------------------------------
// Test: hardDeleteSoftDeleted (no-op but verifies no panic)
// ---------------------------------------------------------------------------

func TestPruner_HardDeleteSoftDeleted_NoOp(t *testing.T) {
	repo := &mockPrunerRepo{}
	p := newTestPruner(repo)

	// Should not panic or error — currently a no-op that logs
	p.hardDeleteSoftDeleted(context.Background())

	assert.Empty(t, repo.deleteCalls, "hard-delete should not call repo.Delete")
}

func TestPruner_pruneAll_CallsHardDeleteSoftDeleted(t *testing.T) {
	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	p := newTestPruner(repo)

	// Should not panic — runs both soft-delete and hard-delete phases
	p.pruneAll(context.Background())

	assert.Equal(t, 1, repo.searchCalls, "search should be called during pruneAll")
}

// ---------------------------------------------------------------------------
// Test: Empty result set (no memories to prune)
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_EmptyResults(t *testing.T) {
	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "no deletions should occur with empty results")
	assert.Equal(t, 1, repo.searchCalls, "search should be called once")
}

// ---------------------------------------------------------------------------
// Test: Nil memory in results is skipped
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_SkipsNilMemory(t *testing.T) {
	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 0.5},
			}, 1, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "nil memory entries should be skipped")
}

// ---------------------------------------------------------------------------
// Test: Search error is logged and continues (no panic)
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_SearchError(t *testing.T) {
	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	p := newTestPruner(repo)

	// Should not panic — logs the error and returns
	p.softDeleteExpired(context.Background())

	assert.Empty(t, repo.deleteCalls, "no deletions should occur after search error")
}

// ---------------------------------------------------------------------------
// Test: Delete error is logged and continues to next memory
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_DeleteErrorContinues(t *testing.T) {
	memA := makePrunerMemory("mem-a", "tenant-1", "first", 3, nil,
		time.Now().Add(-10*24*time.Hour),
		time.Now().Add(-10*24*time.Hour),
	)
	memB := makePrunerMemory("mem-b", "tenant-1", "second", 3, nil,
		time.Now().Add(-10*24*time.Hour),
		time.Now().Add(-10*24*time.Hour),
	)

	deleteAttempted := 0
	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: memA, Score: 0.5},
				{Memory: memB, Score: 0.5},
			}, 2, nil
		},
		deleteFunc: func(ctx context.Context, tenantID, id string) error {
			deleteAttempted++
			if id == "mem-a" {
				return assert.AnError
			}
			return nil
		},
	}
	p := newTestPruner(repo)

	// Should not panic when first delete fails — should continue to second
	p.softDeleteExpired(context.Background())

	assert.Equal(t, 2, deleteAttempted, "both memories should be attempted")
	require.Len(t, repo.deleteCalls, 2, "both should be attempted")
	assert.Equal(t, "mem-a", repo.deleteCalls[0].ID)
	assert.Equal(t, "mem-b", repo.deleteCalls[1].ID)
}

// ---------------------------------------------------------------------------
// Test: Multiple tiers in a single cycle
// ---------------------------------------------------------------------------

func TestPruner_SoftDeleteExpired_MixedTiers(t *testing.T) {
	tier3Old := makePrunerMemory("t3-old", "tenant-1", "tier-3 old", 3, nil,
		time.Now().Add(-10*24*time.Hour),
		time.Now().Add(-10*24*time.Hour),
	)
	tier3Recent := makePrunerMemory("t3-recent", "tenant-1", "tier-3 recent", 3, nil,
		time.Now().Add(-3*24*time.Hour),
		time.Now().Add(-3*24*time.Hour),
	)
	tier0 := makePrunerMemory("t0", "tenant-1", "tier-0", 0, nil,
		time.Now().Add(-365*24*time.Hour),
		time.Now().Add(-365*24*time.Hour),
	)
	tier2Old := makePrunerMemory("t2-old", "tenant-1", "tier-2 old", 2, nil,
		time.Now().Add(-60*24*time.Hour),
		time.Now().Add(-45*24*time.Hour),
	)
	tier2Active := makePrunerMemory("t2-active", "tenant-1", "tier-2 active", 2, nil,
		time.Now().Add(-60*24*time.Hour),
		time.Now().Add(-24*time.Hour),
	)
	tier1Old := makePrunerMemory("t1-old", "tenant-1", "tier-1 old", 1, nil,
		time.Now().Add(-120*24*time.Hour),
		time.Now().Add(-45*24*time.Hour),
	)

	repo := &mockPrunerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: tier3Old, Score: 0.5},
				{Memory: tier3Recent, Score: 0.5},
				{Memory: tier0, Score: 0.5},
				{Memory: tier2Old, Score: 0.5},
				{Memory: tier2Active, Score: 0.5},
				{Memory: tier1Old, Score: 0.5},
			}, 6, nil
		},
	}
	p := newTestPruner(repo)

	p.softDeleteExpired(context.Background())

	// Only tier3Old, tier2Old, and tier1Old should be deleted
	require.Len(t, repo.deleteCalls, 3)
	deletedIDs := make(map[string]bool)
	for _, dc := range repo.deleteCalls {
		deletedIDs[dc.ID] = true
	}
	assert.True(t, deletedIDs["t3-old"], "tier-3 old should be deleted")
	assert.True(t, deletedIDs["t2-old"], "tier-2 old should be deleted")
	assert.True(t, deletedIDs["t1-old"], "tier-1 old should be deleted")
	assert.False(t, deletedIDs["t3-recent"], "tier-3 recent should not be deleted")
	assert.False(t, deletedIDs["t0"], "tier-0 should not be deleted")
	assert.False(t, deletedIDs["t2-active"], "tier-2 active should not be deleted")
}

// ---------------------------------------------------------------------------
// Test: Context cancellation / graceful shutdown
// ---------------------------------------------------------------------------

func TestPruner_Run_ContextCancellation(t *testing.T) {
	repo := &mockPrunerRepo{}
	p := newTestPruner(repo)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Give the goroutine time to start and enter the select
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case <-done:
		// Run returned cleanly — success
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return within 30 seconds after context cancellation")
	}
}

func TestPruner_Run_PreCancelledContext(t *testing.T) {
	repo := &mockPrunerRepo{}
	p := newTestPruner(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5 seconds after pre-cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Test: hasProtectedTag unit tests
// ---------------------------------------------------------------------------

func TestHasProtectedTag(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		expected bool
	}{
		{"nil tags", nil, false},
		{"empty tags", []string{}, false},
		{"critical exact", []string{"critical"}, true},
		{"permanent exact", []string{"permanent"}, true},
		{"critical uppercase", []string{"CRITICAL"}, true},
		{"permanent mixed case", []string{"Permanent"}, true},
		{"critical capitalised", []string{"Critical"}, true},
		{"unrelated tag", []string{"important"}, false},
		{"multiple tags with critical", []string{"important", "critical", "foo"}, true},
		{"multiple tags with permanent", []string{"foo", "permanent", "bar"}, true},
		{"multiple tags without protection", []string{"important", "foo"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasProtectedTag(tt.tags))
		})
	}
}
