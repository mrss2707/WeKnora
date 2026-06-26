package service

import (
	"context"
	"sort"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"go.uber.org/dig"
)

// modelServiceWithFallback wraps the base ModelService with automatic fallback
// for all five typed model getters. When a requested model is not found or
// inactive, resolveDefault picks the best available model of the same type
// (lowest SortOrder, active). CRUD methods are forwarded directly to the base.
//
// This decorator is transparent: no call-site changes are needed, and the
// base ModelService (registered as dig.Name("modelServiceBase")) is untouched.
type modelServiceWithFallback struct {
	inner interfaces.ModelService
	repo  interfaces.ModelRepository
}

// Compile-time interface check.
var _ interfaces.ModelService = (*modelServiceWithFallback)(nil)

// modelServiceWithFallbackParams is the dig.In struct for the decorator
// constructor, used to consume the named "modelServiceBase" dependency.
type modelServiceWithFallbackParams struct {
	dig.In
	Inner interfaces.ModelService `name:"modelServiceBase"`
	Repo  interfaces.ModelRepository
}

// NewModelServiceWithFallback creates the fallback decorator.
// It depends on the NAMED "modelServiceBase" so dig can distinguish the
// decorator from the wrapped base service.
func NewModelServiceWithFallback(
	params modelServiceWithFallbackParams,
) interfaces.ModelService {
	return &modelServiceWithFallback{inner: params.Inner, repo: params.Repo}
}

// resolveDefault returns the best fallback model of the given type for the
// supplied tenant. Sorts by SortOrder ASC, skips non-active, returns the first
// match. Returns nil when no suitable model is found.
func (s *modelServiceWithFallback) resolveDefault(
	ctx context.Context, tenantID uint64, modelType types.ModelType,
) *types.Model {
	models, err := s.repo.List(ctx, tenantID, modelType, "")
	if err != nil || len(models) == 0 {
		return nil
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].SortOrder < models[j].SortOrder
	})
	for _, m := range models {
		if m.Status == types.ModelStatusActive {
			return m
		}
	}
	return nil
}

// --- CRUD methods: delegate to inner ---

func (s *modelServiceWithFallback) CreateModel(ctx context.Context, model *types.Model) error {
	return s.inner.CreateModel(ctx, model)
}

func (s *modelServiceWithFallback) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	return s.inner.GetModelByID(ctx, id)
}

func (s *modelServiceWithFallback) ListModels(ctx context.Context) ([]*types.Model, error) {
	return s.inner.ListModels(ctx)
}

func (s *modelServiceWithFallback) UpdateModel(ctx context.Context, model *types.Model) error {
	return s.inner.UpdateModel(ctx, model)
}

func (s *modelServiceWithFallback) DeleteModel(ctx context.Context, id string) error {
	return s.inner.DeleteModel(ctx, id)
}

func (s *modelServiceWithFallback) UpdateModelCredentials(
	ctx context.Context, id string, apiKey, appSecret *string,
) (*types.Model, error) {
	return s.inner.UpdateModelCredentials(ctx, id, apiKey, appSecret)
}

func (s *modelServiceWithFallback) ClearModelCredential(ctx context.Context, id, field string) error {
	return s.inner.ClearModelCredential(ctx, id, field)
}

// --- Typed getters: try base first, fallback on error ---

// GetChatModel with KnowledgeQA fallback.
func (s *modelServiceWithFallback) GetChatModel(ctx context.Context, modelID string) (chat.Chat, error) {
	result, err := s.inner.GetChatModel(ctx, modelID)
	if err == nil && result != nil {
		return result, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeKnowledgeQA)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetChatModel falling back from %s to %s (%s)",
		modelID, fallback.ID, fallback.Name)
	return s.inner.GetChatModel(ctx, fallback.ID)
}

// GetEmbeddingModel with Embedding fallback.
func (s *modelServiceWithFallback) GetEmbeddingModel(ctx context.Context, modelID string) (embedding.Embedder, error) {
	result, err := s.inner.GetEmbeddingModel(ctx, modelID)
	if err == nil && result != nil {
		return result, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeEmbedding)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetEmbeddingModel falling back from %s to %s (%s)",
		modelID, fallback.ID, fallback.Name)
	return s.inner.GetEmbeddingModel(ctx, fallback.ID)
}

// GetEmbeddingModelForTenant with Embedding fallback (uses explicit tenantID).
func (s *modelServiceWithFallback) GetEmbeddingModelForTenant(
	ctx context.Context, modelID string, tenantID uint64,
) (embedding.Embedder, error) {
	result, err := s.inner.GetEmbeddingModelForTenant(ctx, modelID, tenantID)
	if err == nil && result != nil {
		return result, nil
	}
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeEmbedding)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetEmbeddingModelForTenant falling back from %s to %s (%s) for tenant %d",
		modelID, fallback.ID, fallback.Name, tenantID)
	return s.inner.GetEmbeddingModelForTenant(ctx, fallback.ID, tenantID)
}

// GetRerankModel with Rerank fallback.
func (s *modelServiceWithFallback) GetRerankModel(ctx context.Context, modelID string) (rerank.Reranker, error) {
	result, err := s.inner.GetRerankModel(ctx, modelID)
	if err == nil && result != nil {
		return result, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeRerank)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetRerankModel falling back from %s to %s (%s)",
		modelID, fallback.ID, fallback.Name)
	return s.inner.GetRerankModel(ctx, fallback.ID)
}

// GetVLMModel with VLLM fallback.
func (s *modelServiceWithFallback) GetVLMModel(ctx context.Context, modelID string) (vlm.VLM, error) {
	result, err := s.inner.GetVLMModel(ctx, modelID)
	if err == nil && result != nil {
		return result, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeVLLM)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetVLMModel falling back from %s to %s (%s)",
		modelID, fallback.ID, fallback.Name)
	return s.inner.GetVLMModel(ctx, fallback.ID)
}

// GetASRModel with ASR fallback.
func (s *modelServiceWithFallback) GetASRModel(ctx context.Context, modelID string) (asr.ASR, error) {
	result, err := s.inner.GetASRModel(ctx, modelID)
	if err == nil && result != nil {
		return result, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	fallback := s.resolveDefault(ctx, tenantID, types.ModelTypeASR)
	if fallback == nil {
		return nil, err
	}
	logger.Infof(ctx, "[ModelFallback] GetASRModel falling back from %s to %s (%s)",
		modelID, fallback.ID, fallback.Name)
	return s.inner.GetASRModel(ctx, fallback.ID)
}
