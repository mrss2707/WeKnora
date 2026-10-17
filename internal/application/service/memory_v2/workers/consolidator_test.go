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

	// scopes is returned by ListMemoryScopes; nil means testScope only.
	scopes []Scope

	// Tracking
	hubTenants            []string
	fieldUpdates          []fieldUpdate
	updateErr             error
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
	m.hubTenants = append(m.hubTenants, tenantID)
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
func (m *mockConsolidatorRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

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

// testScope is the single scope the consolidator mock reports by default.
var testScope = Scope{TenantID: "tenant-1", KbID: "kb-1"}

func (m *mockConsolidatorRepo) ListMemoryScopes(context.Context) ([]Scope, error) {
	if m.scopes != nil {
		return m.scopes, nil
	}
	return []Scope{testScope}, nil
}

func newTestConsolidator(repo *mockConsolidatorRepo) *Consolidator {
	return NewConsolidator(repo, nil)
}

type fieldUpdate struct {
	id       string
	fields   map[string]interface{}
	expected *time.Time
}

func (m *mockConsolidatorRepo) UpdateFields(ctx context.Context, tenantID, id string, fields map[string]interface{}) error {
	return m.UpdateFieldsIf(ctx, tenantID, id, fields, nil)
}

func (m *mockConsolidatorRepo) UpdateFieldsIf(_ context.Context, _, id string, fields map[string]interface{}, expected *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	m.fieldUpdates = append(m.fieldUpdates, fieldUpdate{id: id, fields: fields, expected: expected})
	return nil
}

func memAged(id string, v types.MemoryVerdict, importance int, unused time.Duration, accessed *time.Time) *types.AgentMemory {
	t := time.Now().Add(-unused)
	return &types.AgentMemory{ID: id, TenantID: "tenant-1", KbID: "kb-1", Verdict: v, Importance: importance,
		CreatedAt: t, UpdatedAt: t, LastAccessedAt: accessed}
}

func searchReturning(mems ...*types.AgentMemory) func(context.Context, *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	return func(context.Context, *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
		out := make([]*types.MemorySearchResult, len(mems))
		for i, m := range mems {
			out[i] = &types.MemorySearchResult{Memory: m}
		}
		return out, int64(len(out)), nil
	}
}

func TestConsolidator_DecaysOnlyLongUnusedUnprotectedMemories(t *testing.T) {
	year := 400 * 24 * time.Hour
	recentRead := time.Now().Add(-24 * time.Hour)
	repo := &mockConsolidatorRepo{searchFunc: searchReturning(
		memAged("old", types.VerdictNone, 3, year, nil),
		memAged("recent", types.VerdictNone, 3, 30*24*time.Hour, nil),
		memAged("old-but-read", types.VerdictNone, 3, year, &recentRead),
		memAged("decision", types.VerdictDecision, 3, year, nil),
		memAged("floor", types.VerdictNone, 0, year, nil),
		memAged("negative", types.VerdictNone, -2, year, nil),
	)}
	c := newTestConsolidator(repo)

	n, err := c.decayOldMemories(context.Background(), testScope)

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, repo.fieldUpdates, 1)
	assert.Equal(t, "old", repo.fieldUpdates[0].id)
	assert.Equal(t, map[string]interface{}{"importance": 2}, repo.fieldUpdates[0].fields, "one step, not a percentage")
	require.NotNil(t, repo.fieldUpdates[0].expected, "optimistic lock: a concurrent edit wins")
}

func TestConsolidator_DecayUsesAuditedService(t *testing.T) {
	repo := &mockConsolidatorRepo{searchFunc: searchReturning(memAged("old", types.VerdictNone, 3, 400*24*time.Hour, nil))}
	app := &recordingApplier{}
	n, err := newTestConsolidator(repo).WithPatcher(app).decayOldMemories(context.Background(), testScope)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"old importance=2"}, app.patches)
	assert.Empty(t, repo.fieldUpdates, "production decay must not bypass service auditing")
}

func TestConsolidator_DecayConflictIsSkipped(t *testing.T) {
	repo := &mockConsolidatorRepo{updateErr: assert.AnError, searchFunc: searchReturning(memAged("old", types.VerdictNone, 3, 400*24*time.Hour, nil))}
	n, err := newTestConsolidator(repo).decayOldMemories(context.Background(), testScope)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestConsolidator_RunsPerScopeHubScoresOncePerTenant(t *testing.T) {
	var mu sync.Mutex
	var filters []types.MemoryFilter
	repo := &mockConsolidatorRepo{
		scopes: []Scope{{TenantID: "1", KbID: "kb-a"}, {TenantID: "1", KbID: "kb-b"}, {TenantID: "2", KbID: "kb-c"}, {TenantID: "", KbID: "orphan"}},
		searchFunc: func(ctx context.Context, f *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mu.Lock()
			filters = append(filters, *f)
			mu.Unlock()
			return nil, 0, nil
		},
	}

	newTestConsolidator(repo).consolidateAll(context.Background())

	assert.Equal(t, []string{"1", "2"}, repo.hubTenants, "never for an empty tenant")
	require.Len(t, filters, 3)
	for _, f := range filters {
		assert.NotEmpty(t, f.TenantID)
		assert.NotEmpty(t, f.KbID)
	}
}

func TestConsolidator_NeverMergesOrDeletes(t *testing.T) {
	a := memAged("a", types.VerdictNone, 3, time.Hour, nil)
	b := memAged("b", types.VerdictNone, 3, time.Hour, nil)
	b.Content, a.Content = "same", "same"
	repo := &mockConsolidatorRepo{searchFunc: searchReturning(a, b)}

	newTestConsolidator(repo).consolidateAll(context.Background())

	assert.Empty(t, repo.updateCalls)
	assert.Empty(t, repo.deleteCalls)
}

// noScopeConsolidatorRepo hides the scope lister.
type noScopeConsolidatorRepo struct{ interfaces.MemoryRepositoryV2 }

func TestConsolidator_WithoutScopeListerDoesNothing(t *testing.T) {
	inner := &mockConsolidatorRepo{}
	NewConsolidator(noScopeConsolidatorRepo{inner}, nil).consolidateAll(context.Background())
	assert.Zero(t, inner.computeHubScoresCalls)
}

func TestConsolidator_RunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		newTestConsolidator(&mockConsolidatorRepo{}).Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consolidator did not stop")
	}
}
