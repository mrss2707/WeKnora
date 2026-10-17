package workers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockAutoLinkerRepo implements interfaces.MemoryRepositoryV2 for testing the auto-linker.
type mockAutoLinkerRepo struct {
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
	searchCalls  int
	relations    []*types.MemoryRelation
	relationErr  error
	cosineFilter *types.MemoryFilter
	cosineVector []float32
}

func (m *mockAutoLinkerRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockAutoLinkerRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockAutoLinkerRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockAutoLinkerRepo) Delete(ctx context.Context, tenantID, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockAutoLinkerRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	m.mu.Lock()
	m.searchCalls++
	m.mu.Unlock()
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockAutoLinkerRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	m.mu.Lock()
	m.cosineFilter, m.cosineVector = filter, embedding
	m.mu.Unlock()
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockAutoLinkerRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockAutoLinkerRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockAutoLinkerRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockAutoLinkerRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}
func (m *mockAutoLinkerRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockAutoLinkerRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.relationErr != nil {
		return m.relationErr
	}
	m.relations = append(m.relations, rel)
	return nil
}
func (m *mockAutoLinkerRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockAutoLinkerRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockAutoLinkerRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockAutoLinkerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}
func (m *mockAutoLinkerRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

// mockAutoLinkerEmbedder implements embedding.Embedder for testing the auto-linker.
type mockAutoLinkerEmbedder struct {
	mu         sync.Mutex
	embedFunc  func(ctx context.Context, text string) ([]float32, error)
	embedCalls int
	lastText   string
}

func (m *mockAutoLinkerEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	m.embedCalls++
	m.lastText = text
	m.mu.Unlock()
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockAutoLinkerEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockAutoLinkerEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockAutoLinkerEmbedder) GetModelName() string { return "mock-linker" }

func (m *mockAutoLinkerEmbedder) GetDimensions() int { return 3 }

func (m *mockAutoLinkerEmbedder) GetModelID() string { return "mock-linker-model" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func linkerMemory(id, kb string, verdict types.MemoryVerdict, vec ...float32) *types.AgentMemory {
	return &types.AgentMemory{
		ID: id, TenantID: "tenant-1", KbID: kb, Content: "content " + id,
		Verdict: verdict, Embedding: pgvector.NewVector(vec),
		Metadata: []byte(`{"embedding_model_id":"emb-1"}`),
	}
}

func neighbours(rs ...*types.MemorySearchResult) func(context.Context, *types.MemoryFilter, []float32, int) ([]*types.MemorySearchResult, error) {
	return func(context.Context, *types.MemoryFilter, []float32, int) ([]*types.MemorySearchResult, error) {
		return rs, nil
	}
}

func hit(m *types.AgentMemory, score float64) *types.MemorySearchResult {
	return &types.MemorySearchResult{Memory: m, Score: score}
}

type edge struct {
	from, to, kind string
}

func edges(repo *mockAutoLinkerRepo) []edge {
	out := []edge{}
	for _, r := range repo.relations {
		out = append(out, edge{r.FromUUID, r.ToUUID, r.RelationType})
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAutoLinker_LinksNeighboursAboveThresholdUsingStoredVector(t *testing.T) {
	m := linkerMemory("new", "kb", types.VerdictNone, 1, 0, 0)
	repo := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(
		hit(linkerMemory("new", "kb", types.VerdictNone), 1.0), // self
		hit(linkerMemory("close", "kb", types.VerdictNone), 0.84),
		hit(linkerMemory("edge", "kb", types.VerdictNone), DefaultLinkThreshold),
		hit(linkerMemory("far", "kb", types.VerdictNone), 0.55),
	)}
	emb := &mockAutoLinkerEmbedder{}
	n := NewAutoLinker(repo, emb).LinkMemory(context.Background(), m)

	assert.Equal(t, 2, n)
	assert.Equal(t, []edge{{"close", "new", "related_to"}, {"edge", "new", "related_to"}}, edges(repo), "related_to stored in canonical (sorted) order")
	assert.InDelta(t, 0.84, repo.relations[0].Weight, 1e-9, "weight is the similarity")
	assert.Zero(t, emb.embedCalls, "the stored vector is reused; no model call")
	assert.Equal(t, []float32{1, 0, 0}, repo.cosineVector)
	assert.Equal(t, &types.MemoryFilter{TenantID: "tenant-1", KbID: "kb", EmbeddingModelID: "emb-1"}, repo.cosineFilter,
		"neighbours come from the same tenant, knowledge base and embedding model")
}

func TestAutoLinker_DecisionJustifiesInBothDirections(t *testing.T) {
	decision := linkerMemory("decision", "kb", types.VerdictDecision)
	repo := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(decision, 0.8))}
	NewAutoLinker(repo, nil).LinkMemory(context.Background(), linkerMemory("note", "kb", types.VerdictNone, 1))
	assert.Equal(t, []edge{{"decision", "note", "related_to"}, {"decision", "note", "justifies"}}, edges(repo))

	repo = &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(linkerMemory("note", "kb", types.VerdictNone), 0.8))}
	NewAutoLinker(repo, nil).LinkMemory(context.Background(), linkerMemory("decision", "kb", types.VerdictDecision, 1))
	assert.Equal(t, []edge{{"decision", "note", "related_to"}, {"decision", "note", "justifies"}}, edges(repo))
}

func TestAutoLinker_RelatedToIsSymmetric(t *testing.T) {
	ab := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(linkerMemory("b", "kb", types.VerdictNone), 0.9))}
	NewAutoLinker(ab, nil).LinkMemory(context.Background(), linkerMemory("a", "kb", types.VerdictNone, 1))
	ba := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(linkerMemory("a", "kb", types.VerdictNone), 0.9))}
	NewAutoLinker(ba, nil).LinkMemory(context.Background(), linkerMemory("b", "kb", types.VerdictNone, 1))
	assert.Equal(t, edges(ab), edges(ba), "linking from either side yields the same edge")
}

func TestAutoLinker_NeverCrossesTenantOrKnowledgeBase(t *testing.T) {
	other := linkerMemory("other-kb", "kb-2", types.VerdictNone)
	foreign := linkerMemory("other-tenant", "kb", types.VerdictNone)
	foreign.TenantID = "tenant-2"
	repo := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(other, 0.99), hit(foreign, 0.99))}

	assert.Zero(t, NewAutoLinker(repo, nil).LinkMemory(context.Background(), linkerMemory("m", "kb", types.VerdictNone, 1)))
	assert.Empty(t, repo.relations)
}

func TestAutoLinker_EmbedsOnlyWhenNoStoredVector(t *testing.T) {
	repo := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(linkerMemory("n", "kb", types.VerdictNone), 0.9))}
	emb := &mockAutoLinkerEmbedder{}
	m := linkerMemory("m", "kb", types.VerdictNone) // zero-length vector

	NewAutoLinker(repo, emb).LinkMemory(context.Background(), m)

	assert.Equal(t, 1, emb.embedCalls)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, repo.cosineVector)
	assert.Len(t, repo.relations, 1)
}

func TestAutoLinker_FailuresNeverPanic(t *testing.T) {
	ctx := context.Background()
	assert.Zero(t, NewAutoLinker(&mockAutoLinkerRepo{}, nil).LinkMemory(ctx, nil))
	assert.Zero(t, NewAutoLinker(&mockAutoLinkerRepo{}, nil).LinkMemory(ctx, &types.AgentMemory{TenantID: "t"}))
	assert.Zero(t, NewAutoLinker(&mockAutoLinkerRepo{}, nil).LinkMemory(ctx, linkerMemory("m", "kb", types.VerdictNone)),
		"no stored vector and no embedder")

	searchErr := &mockAutoLinkerRepo{cosineSearchFunc: func(context.Context, *types.MemoryFilter, []float32, int) ([]*types.MemorySearchResult, error) {
		return nil, assert.AnError
	}}
	assert.Zero(t, NewAutoLinker(searchErr, nil).LinkMemory(ctx, linkerMemory("m", "kb", types.VerdictNone, 1)))

	embErr := &mockAutoLinkerEmbedder{embedFunc: func(context.Context, string) ([]float32, error) { return nil, assert.AnError }}
	assert.Zero(t, NewAutoLinker(&mockAutoLinkerRepo{}, embErr).LinkMemory(ctx, linkerMemory("m", "kb", types.VerdictNone)))

	relErr := &mockAutoLinkerRepo{relationErr: assert.AnError, cosineSearchFunc: neighbours(hit(linkerMemory("n", "kb", types.VerdictNone), 0.9))}
	assert.Zero(t, NewAutoLinker(relErr, nil).LinkMemory(ctx, linkerMemory("m", "kb", types.VerdictNone, 1)))
}

func TestAutoLinker_CustomThreshold(t *testing.T) {
	repo := &mockAutoLinkerRepo{cosineSearchFunc: neighbours(hit(linkerMemory("n", "kb", types.VerdictNone), 0.7))}
	assert.Zero(t, NewAutoLinker(repo, nil).WithThreshold(0.8).LinkMemory(context.Background(), linkerMemory("m", "kb", types.VerdictNone, 1)))
}

func TestCosineSimilarity(t *testing.T) {
	assert.InDelta(t, 1.0, cosineSimilarity([]float32{1, 2}, []float32{2, 4}), 1e-9)
	assert.InDelta(t, 0.0, cosineSimilarity([]float32{1, 0}, []float32{0, 1}), 1e-9)
	assert.Zero(t, cosineSimilarity([]float32{0, 0}, []float32{1, 1}))
	assert.Zero(t, cosineSimilarity([]float32{1}, []float32{1, 2}))
}
