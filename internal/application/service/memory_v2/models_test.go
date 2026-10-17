package memory_v2

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tenantModelService serves per-tenant model catalogs and records which
// (tenant, model) every client was built for.
type tenantModelService struct {
	serviceLifecycleModelServiceFake
	mu       sync.Mutex
	catalogs map[uint64][]*types.Model
	built    []string
}

func (m *tenantModelService) ListModels(ctx context.Context) ([]*types.Model, error) {
	tid, _ := types.TenantIDFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.catalogs[tid], nil
}

func (m *tenantModelService) GetEmbeddingModelForTenant(_ context.Context, modelID string, tenantID uint64) (embedding.Embedder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.built = append(m.built, "emb:"+modelID+"@"+tenantKey(tenantID))
	return &mockEmbedder{}, nil
}

func (m *tenantModelService) GetChatModel(ctx context.Context, modelID string) (chat.Chat, error) {
	tid, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, errors.New("no tenant in context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.built = append(m.built, "chat:"+modelID+"@"+tenantKey(tid))
	return &mockChat{}, nil
}

func tenantKey(id uint64) string {
	return map[uint64]string{1: "1", 2: "2"}[id]
}

type fakeKBs map[string]*types.KnowledgeBase

func (f fakeKBs) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if kb, ok := f[id]; ok {
		return kb, nil
	}
	return nil, errors.New("knowledge base not found")
}

func newResolverFixture() (*modelResolver, *tenantModelService) {
	ms := &tenantModelService{catalogs: map[uint64][]*types.Model{
		1: {{ID: "t1-chat", Type: types.ModelTypeKnowledgeQA}, {ID: "t1-emb", Type: types.ModelTypeEmbedding}},
		2: {{ID: "t2-emb", Type: types.ModelTypeEmbedding}, {ID: "t2-chat", Type: types.ModelTypeKnowledgeQA}},
	}}
	kbs := fakeKBs{
		"kb-1":    {ID: "kb-1", TenantID: 1, EmbeddingModelID: "kb1-emb", SummaryModelID: "kb1-chat"},
		"kb-1-nm": {ID: "kb-1-nm", TenantID: 1},
		"kb-2":    {ID: "kb-2", TenantID: 2, EmbeddingModelID: "kb2-emb", SummaryModelID: "kb2-chat"},
	}
	return newModelResolver(ms, kbs), ms
}

func TestModelResolver_KnowledgeBaseModelsWin(t *testing.T) {
	r, ms := newResolverFixture()
	ctx := context.Background()

	_, embID, err := r.Embedder(ctx, "1", "kb-1")
	require.NoError(t, err)
	assert.Equal(t, "kb1-emb", embID)
	_, err = r.Chat(ctx, "1", "kb-1")
	require.NoError(t, err)

	assert.Equal(t, []string{"emb:kb1-emb@1", "chat:kb1-chat@1"}, ms.built)
}

func TestModelResolver_FallsBackToTenantDefaults(t *testing.T) {
	r, ms := newResolverFixture()
	ctx := context.Background()

	_, embID, err := r.Embedder(ctx, "1", "kb-1-nm")
	require.NoError(t, err)
	assert.Equal(t, "t1-emb", embID, "KB without models uses the tenant's embedding model")
	_, _, err = r.Embedder(ctx, "2", "")
	require.NoError(t, err)
	_, err = r.Chat(ctx, "2", "")
	require.NoError(t, err)

	assert.Equal(t, []string{"emb:t1-emb@1", "emb:t2-emb@2", "chat:t2-chat@2"}, ms.built)
}

func TestModelResolver_NeverUsesAnotherTenantsKnowledgeBase(t *testing.T) {
	r, ms := newResolverFixture()

	// A memory of tenant 1 naming tenant 2's KB must not pick up its models.
	_, embID, err := r.Embedder(context.Background(), "1", "kb-2")
	require.NoError(t, err)
	assert.Equal(t, "t1-emb", embID)
	assert.Equal(t, []string{"emb:t1-emb@1"}, ms.built)
}

func TestModelResolver_CachesPerTenantAndExpires(t *testing.T) {
	r, ms := newResolverFixture()
	now := time.Unix(1_000_000, 0)
	r.now = func() time.Time { return now }
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _, err := r.Embedder(ctx, "1", "kb-1")
		require.NoError(t, err)
	}
	_, _, err := r.Embedder(ctx, "2", "kb-2")
	require.NoError(t, err)
	assert.Equal(t, []string{"emb:kb1-emb@1", "emb:kb2-emb@2"}, ms.built, "one client per (tenant, model)")

	now = now.Add(modelCacheTTL + time.Second)
	_, _, err = r.Embedder(ctx, "1", "kb-1")
	require.NoError(t, err)
	assert.Len(t, ms.built, 3, "expired entries are rebuilt so model changes apply")
}

func TestModelResolver_Errors(t *testing.T) {
	r, _ := newResolverFixture()
	ctx := context.Background()

	_, _, err := r.Embedder(ctx, "tenant-a", "")
	assert.ErrorContains(t, err, "invalid tenant id")

	empty := newModelResolver(&tenantModelService{catalogs: map[uint64][]*types.Model{}}, nil)
	_, _, err = empty.Embedder(ctx, "1", "")
	assert.ErrorIs(t, err, errNoModel)
	_, err = empty.Chat(ctx, "1", "")
	assert.ErrorIs(t, err, errNoModel)

	var nilResolver *modelResolver
	_, _, err = nilResolver.Embedder(ctx, "1", "")
	assert.ErrorContains(t, err, "model service not available")
}

func TestSaveMemory_RecordsEmbeddingModelAndUsesScopeModel(t *testing.T) {
	r, ms := newResolverFixture()
	repo := &serviceLifecycleRepoFake{}
	svc := NewMemoryServiceV2(repo, ms, enabledMemoryConfig(), nil)
	svc.resolver = r

	_, err := svc.SaveMemory(context.Background(), &types.AgentMemory{
		TenantID: "1", KbID: "kb-1", Content: "Deploys run on Wednesday mornings",
	})
	require.NoError(t, err)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.created, 1)
	assert.Equal(t, "kb1-emb", metadataString(repo.created[0].Metadata, metadataEmbeddingModel))
	assert.Contains(t, ms.built, "emb:kb1-emb@1")
}

func TestSetMetadataString_KeepsOtherKeys(t *testing.T) {
	out := setMetadataString([]byte(`{"source":"mcp"}`), metadataEmbeddingModel, "m1")
	assert.JSONEq(t, `{"source":"mcp","embedding_model_id":"m1"}`, string(out))
	assert.JSONEq(t, `{"embedding_model_id":"m1"}`, string(setMetadataString([]byte("not json"), metadataEmbeddingModel, "m1")))
	assert.Equal(t, "", metadataString(nil, metadataEmbeddingModel))
}
