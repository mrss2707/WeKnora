package memory_v2

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceLifecycleRepoFake struct {
	mu sync.Mutex

	cacheInvalidator interfaces.CacheInvalidator
	created          []*types.AgentMemory
	searchFilters    []*types.MemoryFilter
	cosineFilters    []*types.MemoryFilter
	tryLockCalls     []string
	unlockCalls      []string
	embeddingDim     int
	fingerprintMem   *types.AgentMemory
	searchResults    []*types.MemorySearchResult
	searchErr        error
	cosineResults    []*types.MemorySearchResult
	cosineErr        error
	hardDeleteRows   int64
	hardDeleteErr    error
	hubScoreCalls    int
	scopes           []types.MemoryScope
}

// ListMemoryScopes reports the configured scopes (none by default).
func (r *serviceLifecycleRepoFake) ListMemoryScopes(context.Context) ([]types.MemoryScope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.MemoryScope(nil), r.scopes...), nil
}

var _ interfaces.MemoryRepositoryV2 = (*serviceLifecycleRepoFake)(nil)

func (r *serviceLifecycleRepoFake) Create(ctx context.Context, memory *types.AgentMemory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cpy := *memory
	r.created = append(r.created, &cpy)
	return nil
}

func (r *serviceLifecycleRepoFake) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	return nil, nil
}

func (r *serviceLifecycleRepoFake) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fingerprintMem, nil
}

func (r *serviceLifecycleRepoFake) Update(ctx context.Context, memory *types.AgentMemory) error {
	return nil
}

func (r *serviceLifecycleRepoFake) Delete(ctx context.Context, tenantID, id string) error {
	return nil
}

func (r *serviceLifecycleRepoFake) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}

func (r *serviceLifecycleRepoFake) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}

func (r *serviceLifecycleRepoFake) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}

func (r *serviceLifecycleRepoFake) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	r.mu.Lock()
	if filter != nil {
		cpy := *filter
		r.searchFilters = append(r.searchFilters, &cpy)
	} else {
		r.searchFilters = append(r.searchFilters, nil)
	}
	results := append([]*types.MemorySearchResult(nil), r.searchResults...)
	err := r.searchErr
	r.mu.Unlock()
	return results, int64(len(results)), err
}

func (r *serviceLifecycleRepoFake) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	r.mu.Lock()
	if filter != nil {
		cpy := *filter
		r.cosineFilters = append(r.cosineFilters, &cpy)
	} else {
		r.cosineFilters = append(r.cosineFilters, nil)
	}
	results := append([]*types.MemorySearchResult(nil), r.cosineResults...)
	err := r.cosineErr
	r.mu.Unlock()
	return results, err
}

func (r *serviceLifecycleRepoFake) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	r.mu.Lock()
	r.tryLockCalls = append(r.tryLockCalls, tenantID)
	r.mu.Unlock()
	return true, nil
}

func (r *serviceLifecycleRepoFake) UnlockDreamer(ctx context.Context, tenantID string) error {
	r.mu.Lock()
	r.unlockCalls = append(r.unlockCalls, tenantID)
	r.mu.Unlock()
	return nil
}

func (r *serviceLifecycleRepoFake) TryScopeLock(ctx context.Context, tenantID, kbID, workerID string, ttl time.Duration) (bool, *types.DreamerState, error) {
	r.mu.Lock()
	r.tryLockCalls = append(r.tryLockCalls, tenantID+"/"+kbID)
	r.mu.Unlock()
	return true, &types.DreamerState{TenantID: tenantID, KbID: kbID}, nil
}

func (r *serviceLifecycleRepoFake) HeartbeatScopeLock(context.Context, string, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (r *serviceLifecycleRepoFake) ReleaseScopeLock(ctx context.Context, tenantID, kbID, workerID string, ran bool, stats map[string]interface{}) error {
	r.mu.Lock()
	r.unlockCalls = append(r.unlockCalls, tenantID+"/"+kbID)
	r.mu.Unlock()
	return nil
}

func (r *serviceLifecycleRepoFake) ScopeChangedSince(context.Context, string, string, *time.Time) (bool, error) {
	return true, nil
}

func (r *serviceLifecycleRepoFake) LeastRecentlyDreamed(context.Context, string, string, int) ([]*types.AgentMemory, error) {
	return nil, nil
}

func (r *serviceLifecycleRepoFake) MarkDreamed(context.Context, string, []string, time.Time) error {
	return nil
}

func (r *serviceLifecycleRepoFake) ComputeHubScores(ctx context.Context, tenantID string) error {
	r.mu.Lock()
	r.hubScoreCalls++
	r.mu.Unlock()
	return nil
}

func (r *serviceLifecycleRepoFake) hubScoreCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hubScoreCalls
}

func (r *serviceLifecycleRepoFake) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return r.hardDeleteRows, r.hardDeleteErr
}

func (r *serviceLifecycleRepoFake) InvalidateResultCache(ctx context.Context, tenantID string) {}

func (r *serviceLifecycleRepoFake) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheInvalidator = invalidator
}

func (r *serviceLifecycleRepoFake) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.embeddingDim, nil
}

type serviceLifecycleModelServiceFake struct {
	mu sync.Mutex

	models        []*types.Model
	listErr       error
	listPanic     interface{}
	embedder      embedding.Embedder
	embedderErr   error
	chatModel     chat.Chat
	chatErr       error
	listCalls     int
	embedderCalls int
	chatCalls     int
}

var _ interfaces.ModelService = (*serviceLifecycleModelServiceFake)(nil)

func (m *serviceLifecycleModelServiceFake) CreateModel(ctx context.Context, model *types.Model) error {
	return nil
}

func (m *serviceLifecycleModelServiceFake) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	return nil, nil
}

func (m *serviceLifecycleModelServiceFake) ListModels(ctx context.Context) ([]*types.Model, error) {
	m.mu.Lock()
	m.listCalls++
	panicValue := m.listPanic
	err := m.listErr
	models := append([]*types.Model(nil), m.models...)
	m.mu.Unlock()
	if panicValue != nil {
		panic(panicValue)
	}
	return models, err
}

func (m *serviceLifecycleModelServiceFake) UpdateModel(ctx context.Context, model *types.Model) error {
	return nil
}
func (m *serviceLifecycleModelServiceFake) DeleteModel(ctx context.Context, id string) error {
	return nil
}
func (m *serviceLifecycleModelServiceFake) UpdateModelCredentials(ctx context.Context, id string, apiKey, appSecret *string) (*types.Model, error) {
	return nil, nil
}
func (m *serviceLifecycleModelServiceFake) ClearModelCredential(ctx context.Context, id, field string) error {
	return nil
}
func (m *serviceLifecycleModelServiceFake) GetEmbeddingModel(ctx context.Context, modelID string) (embedding.Embedder, error) {
	m.mu.Lock()
	m.embedderCalls++
	embedder := m.embedder
	err := m.embedderErr
	m.mu.Unlock()
	return embedder, err
}
func (m *serviceLifecycleModelServiceFake) GetEmbeddingModelForTenant(ctx context.Context, modelID string, tenantID uint64) (embedding.Embedder, error) {
	return m.GetEmbeddingModel(ctx, modelID)
}
func (m *serviceLifecycleModelServiceFake) GetRerankModel(ctx context.Context, modelID string) (rerank.Reranker, error) {
	return nil, nil
}
func (m *serviceLifecycleModelServiceFake) GetChatModel(ctx context.Context, modelID string) (chat.Chat, error) {
	m.mu.Lock()
	m.chatCalls++
	chatModel := m.chatModel
	err := m.chatErr
	m.mu.Unlock()
	return chatModel, err
}
func (m *serviceLifecycleModelServiceFake) GetVLMModel(ctx context.Context, modelID string) (vlm.VLM, error) {
	return nil, nil
}
func (m *serviceLifecycleModelServiceFake) GetASRModel(ctx context.Context, modelID string) (asr.ASR, error) {
	return nil, nil
}

func memoryLifecycleModels() []*types.Model {
	return []*types.Model{
		{ID: "embed-model", Type: types.ModelTypeEmbedding},
		{ID: "chat-model", Type: types.ModelTypeKnowledgeQA},
	}
}

func TestNewMemoryServiceV2_WiresScopedWorkersWithoutLegacyCache(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	config := types.DefaultMemoryV2Config()
	svc := NewMemoryServiceV2(repo, nil, config, []string{"legacy-scope-is-ignored"})

	assert.Nil(t, repo.cacheInvalidator, "no unused cache or immortal cleanup goroutine")
	assert.NotNil(t, svc.tokenBudget)
	assert.NotNil(t, svc.pruner)
	assert.NotNil(t, svc.healthChecker)
}

func TestInitWorkers_ConstructsAllWorkersWithoutAnyModel(t *testing.T) {
	// Construction must not depend on models, a tenant or a request: a failing
	// model catalog leaves the workers in place (they resolve models per scope).
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
		listErr: errors.New("model catalog unavailable"),
	}, types.DefaultMemoryV2Config(), nil)

	svc.initWorkers()

	assert.NotNil(t, svc.entityExtractor)
	assert.NotNil(t, svc.autoLinker)
	assert.NotNil(t, svc.consolidator)
	assert.NotNil(t, svc.dreamer)
	assert.NotNil(t, svc.pruner)
}

func TestInitWorkers_ConcurrentCallsBuildOnce(t *testing.T) {
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{}, types.DefaultMemoryV2Config(), nil)

	var wg sync.WaitGroup
	seen := make([]*workers.DreamerWorker, 16)
	for i := range seen {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc.initWorkers()
			svc.workerMu.Lock()
			seen[i] = svc.dreamer
			svc.workerMu.Unlock()
		}(i)
	}
	wg.Wait()
	for _, d := range seen {
		assert.Same(t, seen[0], d, "workers are built exactly once")
	}
}

func TestStartWorkers_UsesServerContextNotRequest(t *testing.T) {
	repo := &serviceLifecycleRepoFake{scopes: []types.MemoryScope{{TenantID: "1", KbID: "kb-1"}}}
	cfg := types.DefaultMemoryV2Config()
	cfg.WorkerMutations = true
	svc := NewMemoryServiceV2(repo, &serviceLifecycleModelServiceFake{}, cfg, nil)

	serverCtx, stop := context.WithCancel(context.Background())
	svc.StartWorkers(serverCtx)

	// A finished request never stops background workers: only the server
	// context (via Cleanup) does.
	reqCtx, endRequest := context.WithCancel(context.Background())
	_, _ = svc.SaveMemory(reqCtx, &types.AgentMemory{TenantID: "1", Content: "short"})
	endRequest()

	require.Eventually(t, func() bool { return repo.hubScoreCallCount() > 0 }, time.Second, 10*time.Millisecond,
		"consolidator keeps running after the request that preceded it ended")
	stop()
	svc.Cleanup()
}

func TestAddEpisode_EmptyMessagesNoSave(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	svc := &MemoryServiceV2Impl{
		repo:        repo,
		embedder:    &mockEmbedder{},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}
	svc.config.LintOnWrite.Enabled = false

	require.NoError(t, svc.AddEpisode(context.Background(), "tenant-1", "user-1", "session-1", nil))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Empty(t, repo.created)
	assert.Empty(t, repo.searchFilters)
}

func TestAddEpisode_SavesEpisodicMemoryWithTenantUserSession(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	svc := &MemoryServiceV2Impl{
		repo:        repo,
		embedder:    &mockEmbedder{},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}
	svc.config.LintOnWrite.Enabled = false

	messages := []types.Message{
		{Role: "user", Content: "today an onboarding event happened and I remember the detail"},
		{Role: "assistant", Content: "recorded the episodic memory for future recall"},
	}

	require.NoError(t, svc.AddEpisode(context.Background(), "tenant-1", "user-1", "session-1", messages))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.created, 1)
	created := repo.created[0]
	assert.Equal(t, "tenant-1", created.TenantID)
	assert.Equal(t, "user-1", created.UserID)
	assert.Equal(t, "session-1", created.SessionID)
	assert.Equal(t, "user: today an onboarding event happened and I remember the detail\nassistant: recorded the episodic memory for future recall\n", created.Content)
	assert.Equal(t, "episodic", created.MemoryType)
	assert.Equal(t, 1, created.Importance)
	assert.Equal(t, 2, created.Tier)
}

func TestConsolidateDream_DreamerNilReturnsEmpty(t *testing.T) {
	svc := &MemoryServiceV2Impl{}

	result, err := svc.ConsolidateDream(context.Background(), "tenant-1")

	require.NoError(t, err)
	assert.Equal(t, &types.DreamResult{}, result)
}

func TestAssessHealth_DelegatesWithEmbedderDimension(t *testing.T) {
	repo := &serviceLifecycleRepoFake{
		embeddingDim: 2,
		searchResults: []*types.MemorySearchResult{{Memory: &types.AgentMemory{
			ID:         "mem-1",
			TenantID:   "tenant-1",
			Content:    "healthy tagged memory content",
			Tags:       []string{"tag"},
			Importance: 3,
			HubScore:   5,
			Verdict:    types.VerdictNone,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}}},
	}
	svc := &MemoryServiceV2Impl{
		repo:          repo,
		embedder:      &mockEmbedder{},
		config:        types.DefaultMemoryV2Config(),
		tokenBudget:   NewTokenBudgetManager(),
		healthChecker: workers.NewHealthChecker(repo),
	}

	issues, err := svc.AssessHealth(context.Background(), "tenant-1", "kb-1")

	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "embedding_dimension_mismatch", issues[0].Type)
	assert.Equal(t, "critical", issues[0].Severity)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.NotEmpty(t, repo.searchFilters)
	assert.Equal(t, "tenant-1", repo.searchFilters[0].TenantID)
	assert.Equal(t, "kb-1", repo.searchFilters[0].KbID)
}

func TestServiceLifecycleRepoFake_RecordsDreamerLockCalls(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	worker := workers.NewDreamerWorker(repo, &mockChat{}, types.DreamerConfig{Enabled: true}, []string{"tenant-1"})

	_, err := worker.RunScopePass(context.Background(), "tenant-1", "kb-1")
	require.NoError(t, err)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Equal(t, []string{"tenant-1/kb-1"}, repo.tryLockCalls)
	assert.Equal(t, []string{"tenant-1/kb-1"}, repo.unlockCalls)
}

func TestServiceLifecycleModelServiceFake_ListError(t *testing.T) {
	modelSvc := &serviceLifecycleModelServiceFake{listErr: fmt.Errorf("catalog down")}
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, modelSvc, types.DefaultMemoryV2Config(), nil)

	_, _, err := svc.embedderFor(context.Background(), "1", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog down")
}

// CopyModel satisfies main's interfaces.ModelService (unused by these tests).
func (m *serviceLifecycleModelServiceFake) CopyModel(ctx context.Context, sourceID, displayName string) (*types.Model, error) {
	return nil, nil
}
