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
	deleteCalls     []deleteCall
	searchCalls     int
	searchFilters   []types.MemoryFilter
	hardDeleteCalls int
	hardDeleteRows  int64
	hardDeleteErr   error
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
	m.searchFilters = append(m.searchFilters, *filter)
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
	m.mu.Lock()
	m.hardDeleteCalls++
	rows := m.hardDeleteRows
	err := m.hardDeleteErr
	m.mu.Unlock()
	return rows, err
}
func (m *mockPrunerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}
func (m *mockPrunerRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

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

func (m *mockPrunerRepo) ListMemoryScopes(context.Context) ([]Scope, error) {
	return []Scope{testScope}, nil
}

func newTestPruner(repo *mockPrunerRepo) *Pruner {
	return NewPruner(repo)
}

// recordingDeleter records audited deletes routed through the service.
type recordingDeleter struct{ ids []string }

func (d *recordingDeleter) DeleteMemory(_ context.Context, _, id string) error {
	d.ids = append(d.ids, id)
	return nil
}

func pmem(id string, tier, importance int, age time.Duration, accessCount int, v types.MemoryVerdict, tags ...string) *types.AgentMemory {
	t := time.Now().Add(-age)
	return &types.AgentMemory{ID: id, TenantID: "tenant-1", KbID: "kb-1", Tier: tier, Importance: importance,
		AccessCount: accessCount, Verdict: v, Tags: tags, CreatedAt: t, UpdatedAt: t}
}

func TestPruner_PolicyKeepsEverythingButExpiredAndUnusedEdgeMemories(t *testing.T) {
	day := 24 * time.Hour
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	expired := pmem("expired", 1, 5, 2*day, 3, types.VerdictNone)
	expired.ExpiresAt = &past
	notYet := pmem("not-yet", 3, 0, 90*day, 0, types.VerdictNone)
	notYet.ExpiresAt = &future
	expiredDecision := pmem("expired-decision", 1, 5, 2*day, 0, types.VerdictDecision)
	expiredDecision.ExpiresAt = &past

	mems := []*types.AgentMemory{
		expired,
		pmem("edge-unused", 3, 0, 60*day, 0, types.VerdictNone),
		pmem("edge-young", 3, 0, 10*day, 0, types.VerdictNone),
		pmem("edge-read", 3, 0, 60*day, 1, types.VerdictNone),
		pmem("edge-important", 3, 2, 60*day, 0, types.VerdictNone),
		pmem("standard-old", 2, 0, 400*day, 0, types.VerdictNone),
		pmem("core-old", 1, 0, 400*day, 0, types.VerdictNone),
		pmem("critical-0", 0, 0, 400*day, 0, types.VerdictNone),
		pmem("edge-fixed", 3, 0, 60*day, 0, types.VerdictFixed),
		pmem("edge-permanent", 3, 0, 60*day, 0, types.VerdictNone, "Permanent"),
		expiredDecision,
	}
	p := newTestPruner(&mockPrunerRepo{})
	var pruned []string
	for _, m := range mems {
		if p.prunable(m, time.Now()) {
			pruned = append(pruned, m.ID)
		}
	}
	assert.Equal(t, []string{"expired", "edge-unused"}, pruned)
	assert.False(t, p.prunable(notYet, time.Now()), "an explicit future expiry wins over the edge-tier rule")
}

func TestPruner_DeletesThroughAuditedDeleter(t *testing.T) {
	repo := &mockPrunerRepo{searchFunc: func(context.Context, *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
		return []*types.MemorySearchResult{
			{Memory: pmem("edge-unused", 3, 0, 60*24*time.Hour, 0, types.VerdictNone)},
			{Memory: pmem("standard", 2, 0, 400*24*time.Hour, 0, types.VerdictNone)},
		}, 2, nil
	}}
	del := &recordingDeleter{}
	p := newTestPruner(repo).WithDeleter(del)

	n := p.softDeleteExpired(context.Background(), testScope)

	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"edge-unused"}, del.ids)
	assert.Empty(t, repo.deleteCalls, "never bypasses the audited service path")
}

func TestPruner_PruneAllIsScopedAndPurgesPerTenant(t *testing.T) {
	repo := &mockPrunerRepo{hardDeleteRows: 2}
	newTestPruner(repo).pruneAll(context.Background())

	assert.Equal(t, 1, repo.searchCalls)
	assert.Equal(t, 1, repo.hardDeleteCalls, "hard-delete once per tenant")
	require.Len(t, repo.searchFilters, 1)
	assert.Equal(t, testScope.TenantID, repo.searchFilters[0].TenantID)
	assert.Equal(t, testScope.KbID, repo.searchFilters[0].KbID)
}

func TestPruner_ErrorsAreLoggedNotFatal(t *testing.T) {
	repo := &mockPrunerRepo{hardDeleteErr: assert.AnError, searchFunc: func(context.Context, *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
		return nil, 0, assert.AnError
	}}
	p := newTestPruner(repo)
	assert.Zero(t, p.softDeleteExpired(context.Background(), testScope))
	p.hardDeleteSoftDeleted(context.Background(), "tenant-1")
	assert.Equal(t, 1, repo.hardDeleteCalls)
}

func TestPruner_RunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		newTestPruner(&mockPrunerRepo{}).Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pruner did not stop")
	}
}

func TestHasProtectedTag(t *testing.T) {
	assert.True(t, hasProtectedTag([]string{"x", " CRITICAL "}))
	assert.True(t, hasProtectedTag([]string{"permanent"}))
	assert.False(t, hasProtectedTag([]string{"criticality"}))
	assert.False(t, hasProtectedTag(nil))
}
