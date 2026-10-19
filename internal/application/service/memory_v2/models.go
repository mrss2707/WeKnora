package memory_v2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// KBModelLookup is the slice of the knowledge-base repository Memory V2 needs
// to find a knowledge base's models. It must not filter by tenant: memories of
// a shared knowledge base live in the owner's tenant and use the owner's models.
type KBModelLookup interface {
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
}

// modelCacheTTL bounds how long a resolved model client is reused, so a model
// changed in the UI takes effect without a restart.
const modelCacheTTL = 10 * time.Minute

// errNoModel is returned when no suitable model is configured.
var errNoModel = errors.New("memory V2: no suitable model configured")

// kbModels identifies the models a scope resolves to.
type kbModels struct {
	tenantID         uint64
	embeddingModelID string
	chatModelID      string
}

type cachedClient struct {
	value     interface{}
	expiresAt time.Time
}

// modelResolver resolves the embedding and chat models of a (tenant, KB)
// scope. A knowledge base's own embedding/summary models win; without a
// knowledge base (or when it names none) the tenant's first model of the
// type is used. Clients are cached per (tenant, model) with a TTL; nothing is
// shared across tenants.
type modelResolver struct {
	modelService interfaces.ModelService
	kbs          KBModelLookup
	// settings holds per-KB model choices (nil = KB summary model only).
	settings interfaces.MemoryV2ScopeSettingsStore
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]cachedClient
}

func newModelResolver(ms interfaces.ModelService, kbs KBModelLookup) *modelResolver {
	return &modelResolver{modelService: ms, kbs: kbs, now: time.Now, cache: map[string]cachedClient{}}
}

// parseTenantID converts a memory tenant id (VARCHAR) to the numeric id the
// model service uses.
func parseTenantID(tenantID string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(tenantID), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("memory V2: invalid tenant id %q", tenantID)
	}
	return id, nil
}

// scopeModels returns the model ids configured for a scope.
func (r *modelResolver) scopeModels(ctx context.Context, tenantID, kbID string) (kbModels, error) {
	tid, err := parseTenantID(tenantID)
	if err != nil {
		return kbModels{}, err
	}
	out := kbModels{tenantID: tid}
	if kbID = strings.TrimSpace(kbID); kbID != "" && r.kbs != nil {
		kb, err := r.kbs.GetKnowledgeBaseByID(ctx, kbID)
		if err == nil && kb != nil && kb.TenantID == tid {
			out.embeddingModelID = kb.EmbeddingModelID
			out.chatModelID = kb.SummaryModelID
		}
	}
	return out, nil
}

// tenantCtx scopes ctx to the memory's tenant for model lookups. It changes
// repository scope only, never authorization.
func tenantCtx(ctx context.Context, tenantID uint64) context.Context {
	return types.WithExecutionTenant(ctx, tenantID)
}

// firstModelOfType returns the tenant's first model of the given type.
func (r *modelResolver) firstModelOfType(ctx context.Context, tid uint64, mt types.ModelType) (string, error) {
	models, err := r.modelService.ListModels(tenantCtx(ctx, tid))
	if err != nil {
		return "", fmt.Errorf("memory V2: list models: %w", err)
	}
	for _, m := range models {
		if m != nil && m.Type == mt {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("%w: tenant %d has no %s model", errNoModel, tid, mt)
}

func (r *modelResolver) cached(key string) (interface{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cache[key]
	if !ok || r.now().After(c.expiresAt) {
		delete(r.cache, key)
		return nil, false
	}
	return c.value, true
}

func (r *modelResolver) store(key string, v interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = cachedClient{value: v, expiresAt: r.now().Add(modelCacheTTL)}
}

// Embedder returns the embedder for a scope and its model id. The model id is
// recorded on every memory so vectors from different models are never
// compared.
func (r *modelResolver) Embedder(ctx context.Context, tenantID, kbID string) (embedding.Embedder, string, error) {
	if r == nil || r.modelService == nil {
		return nil, "", fmt.Errorf("memory V2: model service not available")
	}
	scope, err := r.scopeModels(ctx, tenantID, kbID)
	if err != nil {
		return nil, "", err
	}
	modelID := scope.embeddingModelID
	if modelID == "" {
		if modelID, err = r.firstModelOfType(ctx, scope.tenantID, types.ModelTypeEmbedding); err != nil {
			return nil, "", err
		}
	}
	key := fmt.Sprintf("emb|%d|%s", scope.tenantID, modelID)
	if v, ok := r.cached(key); ok {
		return v.(embedding.Embedder), modelID, nil
	}
	emb, err := r.modelService.GetEmbeddingModelForTenant(tenantCtx(ctx, scope.tenantID), modelID, scope.tenantID)
	if err != nil {
		return nil, "", fmt.Errorf("memory V2: embedding model %s: %w", modelID, err)
	}
	if emb == nil {
		return nil, "", fmt.Errorf("%w: embedding model %s unavailable", errNoModel, modelID)
	}
	r.store(key, emb)
	return emb, modelID, nil
}

// Chat returns the text-task chat model for a scope.
func (r *modelResolver) Chat(ctx context.Context, tenantID, kbID string) (chat.Chat, error) {
	return r.ChatFor(ctx, tenantID, kbID, types.MemoryModelPurposeText)
}

// ChatFor returns the chat model of a purpose; see chatModelFor for the order.
func (r *modelResolver) ChatFor(ctx context.Context, tenantID, kbID string, purpose types.MemoryModelPurpose) (chat.Chat, error) {
	if r == nil || r.modelService == nil {
		return nil, fmt.Errorf("memory V2: model service not available")
	}
	effective, tid, err := r.chatModelFor(ctx, tenantID, kbID, purpose)
	if err != nil {
		return nil, err
	}
	modelID := effective.ModelID
	key := fmt.Sprintf("chat|%d|%s", tid, modelID)
	if v, ok := r.cached(key); ok {
		return v.(chat.Chat), nil
	}
	ch, err := r.modelService.GetChatModel(tenantCtx(ctx, tid), modelID)
	if err != nil {
		if effective.Source == types.MemoryModelSourceSetting {
			return nil, fmt.Errorf("%w: %s", types.ErrMemoryModelUnavailable, modelID)
		}
		return nil, fmt.Errorf("memory V2: chat model %s: %w", modelID, err)
	}
	if ch == nil {
		return nil, fmt.Errorf("%w: chat model %s unavailable", errNoModel, modelID)
	}
	r.store(key, ch)
	return ch, nil
}
