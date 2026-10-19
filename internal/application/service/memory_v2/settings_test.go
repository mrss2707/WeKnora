package memory_v2

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// purposeModelService adds model lookup (own tenant, built-in, inactive) to
// the per-tenant catalog fake.
type purposeModelService struct {
	*tenantModelService
	models map[string]*types.Model // id -> model; missing = deleted/inactive
}

func (m *purposeModelService) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	tid, _ := types.TenantIDFromContext(ctx)
	model, ok := m.models[id]
	if !ok || (model.TenantID != tid && !model.IsBuiltin) {
		return nil, errors.New("model not found")
	}
	return model, nil
}

func (m *purposeModelService) GetChatModel(ctx context.Context, id string) (chat.Chat, error) {
	return m.tenantModelService.GetChatModel(ctx, id)
}

type memorySettingsStore struct {
	mu   sync.Mutex
	rows map[string]*types.MemoryV2ScopeSettings
}

func (s *memorySettingsStore) GetScopeSettings(_ context.Context, tenantID, kbID string) (*types.MemoryV2ScopeSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.rows[tenantID+"/"+kbID]; ok {
		copy := *row
		return &copy, nil
	}
	return nil, nil
}

func (s *memorySettingsStore) UpsertScopeSettings(_ context.Context, row *types.MemoryV2ScopeSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *row
	s.rows[row.TenantID+"/"+row.KbID] = &copy
	return nil
}

// settingsRepo is a repository that also persists model settings.
type settingsRepo struct {
	*serviceLifecycleRepoFake
	*memorySettingsStore
}

func newPurposeFixture() (*modelResolver, *purposeModelService, *memorySettingsStore) {
	base, ms := newResolverFixture()
	pms := &purposeModelService{tenantModelService: ms, models: map[string]*types.Model{
		"pro":     {ID: "pro", TenantID: 1, Name: "deepseek-v4-pro", Type: types.ModelTypeKnowledgeQA},
		"flash":   {ID: "flash", TenantID: 1, Name: "deepseek-flash", Type: types.ModelTypeKnowledgeQA},
		"shared":  {ID: "shared", TenantID: 99, Name: "builtin", Type: types.ModelTypeKnowledgeQA, IsBuiltin: true},
		"t2-pro":  {ID: "t2-pro", TenantID: 2, Type: types.ModelTypeKnowledgeQA},
		"emb-btn": {ID: "emb-btn", TenantID: 1, Type: types.ModelTypeEmbedding},
	}}
	store := &memorySettingsStore{rows: map[string]*types.MemoryV2ScopeSettings{}}
	r := newModelResolver(pms, base.kbs)
	r.settings = store
	return r, pms, store
}

func TestChatFor_PurposeOrder(t *testing.T) {
	r, ms, store := newPurposeFixture()
	ctx := context.Background()

	_, err := r.ChatFor(ctx, "1", "kb-1", types.MemoryModelPurposeDreamer)
	require.NoError(t, err)
	assert.Equal(t, "chat:kb1-chat@1", ms.built[len(ms.built)-1], "no setting keeps the KB summary model")

	store.rows["1/kb-1"] = &types.MemoryV2ScopeSettings{TenantID: "1", KbID: "kb-1", DreamerModelID: "pro", TextModelID: "flash"}
	_, err = r.ChatFor(ctx, "1", "kb-1", types.MemoryModelPurposeDreamer)
	require.NoError(t, err)
	assert.Equal(t, "chat:pro@1", ms.built[len(ms.built)-1], "a setting applies on the next call, before any cache")
	_, err = r.Chat(ctx, "1", "kb-1")
	require.NoError(t, err)
	assert.Equal(t, "chat:flash@1", ms.built[len(ms.built)-1], "Chat is the text purpose")

	_, err = r.ChatFor(ctx, "1", "kb-1-nm", types.MemoryModelPurposeDreamer)
	require.NoError(t, err)
	assert.Equal(t, "chat:t1-chat@1", ms.built[len(ms.built)-1], "settings are per knowledge base")
}

func TestChatFor_UnavailableChoice(t *testing.T) {
	r, ms, store := newPurposeFixture()
	ctx := context.Background()
	store.rows["1/kb-1"] = &types.MemoryV2ScopeSettings{TenantID: "1", KbID: "kb-1", DreamerModelID: "deleted", TextModelID: "deleted"}

	_, err := r.ChatFor(ctx, "1", "kb-1", types.MemoryModelPurposeDreamer)
	require.ErrorIs(t, err, types.ErrMemoryModelUnavailable, "the Dreamer never falls back to an unchosen model")
	assert.Empty(t, ms.built)

	_, err = r.ChatFor(ctx, "1", "kb-1", types.MemoryModelPurposeText)
	require.NoError(t, err)
	assert.Equal(t, []string{"chat:kb1-chat@1"}, ms.built, "text tasks fall back to the summary model")
}

func TestUpdateModelSettings_Validates(t *testing.T) {
	r, ms, store := newPurposeFixture()
	repo := settingsRepo{&serviceLifecycleRepoFake{}, store}
	svc := NewMemoryServiceV2(repo, ms, enabledMemoryConfig(), nil)
	svc.resolver = r
	ctx := context.Background()

	for _, bad := range []string{"t2-pro", "emb-btn", "deleted"} {
		_, err := svc.UpdateModelSettings(ctx, "1", "kb-1", "u1", types.MemoryV2ModelSettingsUpdate{DreamerModelID: bad})
		assert.ErrorIs(t, err, types.ErrMemoryModelSettingInvalid, bad)
	}
	assert.Empty(t, store.rows, "rejected settings are not stored")

	out, err := svc.UpdateModelSettings(ctx, "1", "kb-1", "u1", types.MemoryV2ModelSettingsUpdate{DreamerModelID: " pro ", TextModelID: "shared"})
	require.NoError(t, err)
	assert.Equal(t, "pro", out.DreamerModelID)
	assert.Equal(t, types.MemoryEffectiveModel{ModelID: "pro", Name: "deepseek-v4-pro", Source: types.MemoryModelSourceSetting}, out.Dreamer)
	assert.Equal(t, types.MemoryModelSourceSetting, out.Text.Source, "built-in models are allowed")
	assert.Equal(t, "u1", store.rows["1/kb-1"].UpdatedBy)

	out, err = svc.UpdateModelSettings(ctx, "1", "kb-1", "u1", types.MemoryV2ModelSettingsUpdate{})
	require.NoError(t, err)
	assert.Equal(t, types.MemoryModelSourceKBSummary, out.Dreamer.Source, "clearing restores the summary model")
	assert.Equal(t, "kb1-chat", out.Dreamer.ModelID)

	store.rows["1/kb-1"].DreamerModelID = "deleted"
	got, err := svc.GetModelSettings(ctx, "1", "kb-1")
	require.NoError(t, err)
	assert.Equal(t, types.MemoryModelSourceUnavailable, got.Dreamer.Source)
}
