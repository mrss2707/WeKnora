package memory_v2

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockModelService — implements interfaces.ModelService for testing ensureWorkers
// ---------------------------------------------------------------------------

// mockModelService implements all 13 methods of interfaces.ModelService.
// The 3 function fields control the core logic; the other 10 return zero values.
type mockModelService struct {
	listModelsFunc          func(ctx context.Context) ([]*types.Model, error)
	getEmbeddingModelFunc   func(ctx context.Context, modelID string) (embedding.Embedder, error)
	getChatModelFunc        func(ctx context.Context, modelID string) (chat.Chat, error)

	listModelsCalls int
}

// Compile-time interface check
var _ interfaces.ModelService = (*mockModelService)(nil)

func (m *mockModelService) CreateModel(ctx context.Context, model *types.Model) error {
	return nil
}

func (m *mockModelService) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	return nil, nil
}

func (m *mockModelService) ListModels(ctx context.Context) ([]*types.Model, error) {
	m.listModelsCalls++
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx)
	}
	return nil, nil
}

func (m *mockModelService) UpdateModel(ctx context.Context, model *types.Model) error {
	return nil
}

func (m *mockModelService) DeleteModel(ctx context.Context, id string) error {
	return nil
}

func (m *mockModelService) UpdateModelCredentials(ctx context.Context, id string, apiKey, appSecret *string) (*types.Model, error) {
	return nil, nil
}

func (m *mockModelService) ClearModelCredential(ctx context.Context, id, field string) error {
	return nil
}

func (m *mockModelService) GetEmbeddingModel(ctx context.Context, modelId string) (embedding.Embedder, error) {
	if m.getEmbeddingModelFunc != nil {
		return m.getEmbeddingModelFunc(ctx, modelId)
	}
	return nil, nil
}

func (m *mockModelService) GetEmbeddingModelForTenant(ctx context.Context, modelId string, tenantID uint64) (embedding.Embedder, error) {
	return nil, nil
}

func (m *mockModelService) GetRerankModel(ctx context.Context, modelId string) (rerank.Reranker, error) {
	return nil, nil
}

func (m *mockModelService) GetChatModel(ctx context.Context, modelId string) (chat.Chat, error) {
	if m.getChatModelFunc != nil {
		return m.getChatModelFunc(ctx, modelId)
	}
	return nil, nil
}

func (m *mockModelService) GetVLMModel(ctx context.Context, modelId string) (vlm.VLM, error) {
	return nil, nil
}

func (m *mockModelService) GetASRModel(ctx context.Context, modelId string) (asr.ASR, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// EnsureWorkers tests
// ---------------------------------------------------------------------------

func newMemoryServiceV2ForWorkerTest(ms *mockModelService) *MemoryServiceV2Impl {
	config := types.DefaultMemoryV2Config()
	// Disable cache warmer to simplify test assertions (no extra goroutine)
	config.CacheWarmer.Enabled = false
	return &MemoryServiceV2Impl{
		repo:         &mockSearchRepo{},
		modelService: ms,
		config:       config,
		tokenBudget:  NewTokenBudgetManager(),
	}
}

// validModels returns a list with both Embedding and KnowledgeQA models.
func validModels() []*types.Model {
	return []*types.Model{
		{ID: "emb-1", Type: types.ModelTypeEmbedding},
		{ID: "chat-1", Type: types.ModelTypeKnowledgeQA},
	}
}

func TestEnsureWorkers_FirstCallSucceeds(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			return validModels(), nil
		},
		getEmbeddingModelFunc: func(ctx context.Context, modelID string) (embedding.Embedder, error) {
			return &mockEmbedder{}, nil
		},
		getChatModelFunc: func(ctx context.Context, modelID string) (chat.Chat, error) {
			return &mockChat{}, nil
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)
	svc.ensureWorkers(context.Background())

	assert.NotNil(t, svc.entityExtractor, "entityExtractor should be initialized")
	assert.NotNil(t, svc.consolidator, "consolidator should be initialized")
	assert.NotNil(t, svc.dreamer, "dreamer should be initialized")
	assert.NotNil(t, svc.cacheWarmer, "cacheWarmer should be initialized")
}

func TestEnsureWorkers_SecondCallIsNoop(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			return validModels(), nil
		},
		getEmbeddingModelFunc: func(ctx context.Context, modelID string) (embedding.Embedder, error) {
			return &mockEmbedder{}, nil
		},
		getChatModelFunc: func(ctx context.Context, modelID string) (chat.Chat, error) {
			return &mockChat{}, nil
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	// First call: succeeds, initializes workers
	svc.ensureWorkers(context.Background())
	require.NotNil(t, svc.entityExtractor)

	// Record the call count
	callsBefore := ms.listModelsCalls

	// Second call: should return immediately (entityExtractor is set)
	svc.ensureWorkers(context.Background())

	assert.Equal(t, callsBefore, ms.listModelsCalls, "ListModels should not be called again")
}

func TestEnsureWorkers_ModelServiceNil(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		repo:         &mockSearchRepo{},
		modelService: nil,
		config:       types.DefaultMemoryV2Config(),
		tokenBudget:  NewTokenBudgetManager(),
	}

	// Should not panic
	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created when modelService is nil")
	assert.Nil(t, svc.consolidator, "consolidator should not be created when modelService is nil")
	assert.Nil(t, svc.dreamer, "dreamer should not be created when modelService is nil")
}

func TestEnsureWorkers_ListModelsPanics(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			panic("no tenant context")
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	// Should not panic — getEmbedder has recover()
	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created when ListModels panics")
	assert.Nil(t, svc.consolidator, "consolidator should not be created when ListModels panics")
}

func TestEnsureWorkers_ListModelsError(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			return nil, fmt.Errorf("database error")
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created when ListModels fails")
	assert.Nil(t, svc.consolidator, "consolidator should not be created when ListModels fails")
}

func TestEnsureWorkers_NoEmbeddingModel(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			// Only chat models, no embedding
			return []*types.Model{
				{ID: "chat-1", Type: types.ModelTypeKnowledgeQA},
			}, nil
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created without embedding model")
	assert.Nil(t, svc.consolidator, "consolidator should not be created without embedding model")
}

func TestEnsureWorkers_NoKnowledgeQAModel(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			// Only embedding models, no KnowledgeQA
			return []*types.Model{
				{ID: "emb-1", Type: types.ModelTypeEmbedding},
			}, nil
		},
		getEmbeddingModelFunc: func(ctx context.Context, modelID string) (embedding.Embedder, error) {
			return &mockEmbedder{}, nil
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	svc.ensureWorkers(context.Background())

	// getEmbedder succeeds, but getChat fails → workers not created
	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created without chat model")
	assert.Nil(t, svc.dreamer, "dreamer should not be created without chat model")
}

func TestEnsureWorkers_GetEmbeddingModelFails(t *testing.T) {
	ms := &mockModelService{
		listModelsFunc: func(ctx context.Context) ([]*types.Model, error) {
			return validModels(), nil
		},
		getEmbeddingModelFunc: func(ctx context.Context, modelID string) (embedding.Embedder, error) {
			return nil, errors.New("embedder init failed")
		},
	}

	svc := newMemoryServiceV2ForWorkerTest(ms)

	svc.ensureWorkers(context.Background())

	assert.Nil(t, svc.entityExtractor, "entityExtractor should not be created when GetEmbeddingModel fails")
	assert.Nil(t, svc.consolidator, "consolidator should not be created when GetEmbeddingModel fails")
}
