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

func (r *serviceLifecycleRepoFake) ComputeHubScores(ctx context.Context, tenantID string) error {
	return nil
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

func TestMemoryCache_SetGetLenKeysInvalidate(t *testing.T) {
	cache := NewMemoryCache()

	cache.Set("tenant:one", "permanent", 0)
	cache.Set("tenant:two", "ttl", 60)

	assert.Equal(t, "permanent", cache.Get("tenant:one"))
	assert.Equal(t, "ttl", cache.Get("tenant:two"))
	assert.Equal(t, 2, cache.Len())
	assert.ElementsMatch(t, []string{"tenant:one", "tenant:two"}, cache.Keys())

	cache.Invalidate("tenant:one")
	assert.Nil(t, cache.Get("tenant:one"))
	assert.Equal(t, 1, cache.Len())

	cache.Set("tenant:three", "prefix", 0)
	cache.Set("other:four", "keep", 0)
	cache.InvalidateByPrefix("tenant:")
	assert.Nil(t, cache.Get("tenant:two"))
	assert.Nil(t, cache.Get("tenant:three"))
	assert.Equal(t, "keep", cache.Get("other:four"))
}

func TestMemoryCache_ExpiredEntries(t *testing.T) {
	cache := NewMemoryCache()
	cache.items["expired"] = cacheEntry{value: "old", expiresAt: time.Now().Add(-time.Second)}
	cache.items["fresh"] = cacheEntry{value: "new", expiresAt: time.Now().Add(time.Hour)}

	assert.Nil(t, cache.Get("expired"))
	assert.Equal(t, 2, cache.Len(), "Get should not evict expired entries by itself")

	cache.evictExpired()
	assert.Nil(t, cache.Get("expired"))
	assert.Equal(t, "new", cache.Get("fresh"))
	assert.Equal(t, 1, cache.Len())
}

func TestMemoryCache_MaxSizeEvictsOne(t *testing.T) {
	cache := NewMemoryCache()
	cache.maxSize = 1

	cache.Set("first", "one", 0)
	cache.Set("second", "two", 0)

	assert.Equal(t, 1, cache.Len())
}

func TestMemoryCache_StartCleanupReturnsOnceButDoesNotAssertSingleton(t *testing.T) {
	cache := NewMemoryCache()
	once := cache.StartCleanup(time.Hour)

	require.NotNil(t, once)
}

func TestNewMemoryServiceV2_WiresCacheWorkersAndTenantIDs(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	config := types.DefaultMemoryV2Config()
	svc := NewMemoryServiceV2(repo, nil, config, []string{"tenant-1", "tenant-2"})

	repo.mu.Lock()
	invalidator := repo.cacheInvalidator
	repo.mu.Unlock()

	assert.Same(t, svc.cache, invalidator)
	assert.NotNil(t, svc.tokenBudget)
	assert.NotNil(t, svc.cache)
	assert.NotNil(t, svc.pruner)
	assert.NotNil(t, svc.healthChecker)
	assert.Equal(t, []string{"tenant-1", "tenant-2"}, svc.tenantIDs)
}

func TestGetEmbedderAndChat_UseModelServiceAndCacheModelList(t *testing.T) {
	modelSvc := &serviceLifecycleModelServiceFake{
		models:    memoryLifecycleModels(),
		embedder:  &mockEmbedder{},
		chatModel: &mockChat{},
	}
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, modelSvc, types.DefaultMemoryV2Config(), nil)

	emb, err := svc.getEmbedder(context.Background())
	require.NoError(t, err)
	require.NotNil(t, emb)

	ch, err := svc.getChat(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ch)

	modelSvc.mu.Lock()
	defer modelSvc.mu.Unlock()
	assert.Equal(t, 1, modelSvc.listCalls)
	assert.Equal(t, 1, modelSvc.embedderCalls)
	assert.Equal(t, 1, modelSvc.chatCalls)
	assert.Len(t, svc.cachedModels, 2)
}

func TestGetEmbedder_ModelServiceNilAndMissingEmbedding(t *testing.T) {
	t.Run("nil model service", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{}
		_, err := svc.getEmbedder(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model service not available")
	})

	t.Run("missing embedding model", func(t *testing.T) {
		svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
			models: []*types.Model{{ID: "chat-model", Type: types.ModelTypeKnowledgeQA}},
		}, types.DefaultMemoryV2Config(), nil)
		_, err := svc.getEmbedder(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no Embedding model configured")
	})
}

func TestGetChat_ModelServiceNilAndMissingChat(t *testing.T) {
	t.Run("nil model service", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{}
		_, err := svc.getChat(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model service not available")
	})

	t.Run("missing chat model", func(t *testing.T) {
		svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
			models: []*types.Model{{ID: "embed-model", Type: types.ModelTypeEmbedding}},
		}, types.DefaultMemoryV2Config(), nil)
		_, err := svc.getChat(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no KnowledgeQA model configured")
	})
}

func TestGetEmbedderAndChat_RecoverListModelsPanic(t *testing.T) {
	for _, tt := range []struct {
		name string
		call func(*MemoryServiceV2Impl) error
		want string
	}{
		{
			name: "embedder",
			call: func(svc *MemoryServiceV2Impl) error {
				_, err := svc.getEmbedder(context.Background())
				return err
			},
			want: "getEmbedder recovered from panic: boom",
		},
		{
			name: "chat",
			call: func(svc *MemoryServiceV2Impl) error {
				_, err := svc.getChat(context.Background())
				return err
			},
			want: "getChat recovered from panic: boom",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{listPanic: "boom"}, types.DefaultMemoryV2Config(), nil)
			err := tt.call(svc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestEnsureWorkers_ModelInitFailureLeavesWorkersNil(t *testing.T) {
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
		listErr: errors.New("model catalog unavailable"),
	}, types.DefaultMemoryV2Config(), nil)

	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor)
	assert.Nil(t, svc.autoLinker)
	assert.Nil(t, svc.dreamer)
	assert.Nil(t, svc.cacheWarmer)
}

func TestEnsureWorkers_InitializesEmbedderDependentWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
		models:    memoryLifecycleModels(),
		embedder:  &mockEmbedder{},
		chatModel: &mockChat{},
	}, types.DefaultMemoryV2Config(), []string{"tenant-1"})

	svc.ensureWorkers(ctx)
	svc.Cleanup()

	assert.NotNil(t, svc.entityExtractor)
	assert.NotNil(t, svc.autoLinker)
	assert.NotNil(t, svc.consolidator)
	assert.NotNil(t, svc.dreamer)
	assert.NotNil(t, svc.cacheWarmer)
}

func TestEnsureWorkers_ConcurrentCallsRaceCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, &serviceLifecycleModelServiceFake{
		models:    memoryLifecycleModels(),
		embedder:  &mockEmbedder{},
		chatModel: &mockChat{},
	}, types.DefaultMemoryV2Config(), []string{"tenant-1"})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.ensureWorkers(ctx)
		}()
	}
	wg.Wait()
	svc.Cleanup()

	assert.NotNil(t, svc.entityExtractor)
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

	worker.RunPass(context.Background(), "tenant-1")

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Equal(t, []string{"tenant-1"}, repo.tryLockCalls)
	assert.Equal(t, []string{"tenant-1"}, repo.unlockCalls)
}

func TestServiceLifecycleModelServiceFake_ListError(t *testing.T) {
	modelSvc := &serviceLifecycleModelServiceFake{listErr: fmt.Errorf("catalog down")}
	svc := NewMemoryServiceV2(&serviceLifecycleRepoFake{}, modelSvc, types.DefaultMemoryV2Config(), nil)

	_, err := svc.getEmbedder(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog down")
}
