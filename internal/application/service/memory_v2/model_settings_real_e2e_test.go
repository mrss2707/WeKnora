//go:build memory_e2e

package memory_v2

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// hostedModelService builds real hosted chat clients for an owner-listed
// provider and records which model each call resolved to.
type hostedModelService struct {
	serviceLifecycleModelServiceFake
	baseURL, apiKey string
	models          map[string]*types.Model
	mu              sync.Mutex
	built           []string
}

func (m *hostedModelService) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	if model, ok := m.models[id]; ok {
		return model, nil
	}
	return nil, os.ErrNotExist
}

func (m *hostedModelService) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	m.mu.Lock()
	m.built = append(m.built, m.models[id].Name)
	m.mu.Unlock()
	return chat.NewRemoteChat(&chat.ChatConfig{Source: types.ModelSourceRemote, BaseURL: m.baseURL,
		ModelName: m.models[id].Name, APIKey: m.apiKey, Provider: "anthropic", ModelID: id})
}

// TestTextModelSettingDrivesEntityExtraction exercises the production text
// path with a real hosted model: per-KB setting -> resolver -> extractor ->
// audited tag patch on PostgreSQL. Only synthetic content is sent.
func TestTextModelSettingDrivesEntityExtraction(t *testing.T) {
	text, dreamer := os.Getenv("MEMV2_E2E_TEXT_MODEL"), os.Getenv("MEMV2_E2E_DREAMER_MODEL")
	if text == "" || dreamer == "" {
		t.Skip("set MEMV2_E2E_TEXT_MODEL and MEMV2_E2E_DREAMER_MODEL (provider anthropic:4)")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(home, ".paicode", "providers.json"))
	require.NoError(t, err)
	var file struct {
		Provider map[string]struct {
			Options struct{ BaseURL, APIKey string }
		}
	}
	require.NoError(t, json.Unmarshal(data, &file))
	provider := file.Provider["anthropic:4"].Options
	require.NotEmpty(t, provider.APIKey)

	ms := &hostedModelService{baseURL: provider.BaseURL, apiKey: provider.APIKey, models: map[string]*types.Model{
		"text":    {ID: "text", TenantID: 1, Name: text, Type: types.ModelTypeKnowledgeQA},
		"dreamer": {ID: "dreamer", TenantID: 1, Name: dreamer, Type: types.ModelTypeKnowledgeQA},
	}}
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	cfg.EntityExtractorEnabled = true
	cfg.Dreamer.Enabled, cfg.PrunerEnabled, cfg.ConsolidatorEnabled = false, false, false
	svc := NewMemoryServiceV2(repository.NewMemoryRepository(pgServiceDB(t)), ms, cfg, nil)
	// Distinct synthetic vectors keep the two memories from deduplicating.
	svc.embedder = &mockEmbedder{embedFunc: func(_ context.Context, text string) ([]float32, error) {
		if strings.Contains(text, "Kubernetes") {
			return []float32{0, 1, 0}, nil
		}
		return []float32{1, 0, 0}, nil
	}}
	// The summary model is the Dreamer's: the text setting must win for extraction.
	svc.SetKBLookup(fakeKBs{"kb": {ID: "kb", TenantID: 1, SummaryModelID: "dreamer"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err = svc.UpdateModelSettings(ctx, "1", "kb", "e2e", types.MemoryV2ModelSettingsUpdate{TextModelID: "text"})
	require.NoError(t, err)

	svc.StartWorkers(ctx)
	contents := []string{
		"Alice Nguyen migrated the Billing API from PostgreSQL 14 to PostgreSQL 17 in Hanoi.",
		"Nhóm Payments của công ty Acme dùng Kubernetes và Redis ở Singapore.",
	}
	var ids []string
	for _, content := range contents {
		res, err := svc.SaveMemory(ctx, &types.AgentMemory{TenantID: "1", KbID: "kb", Content: content})
		require.NoError(t, err)
		ids = append(ids, res.Memory.ID)
	}
	require.NotEqual(t, ids[0], ids[1])
	svc.Cleanup() // flushes the buffered batch through the real model

	ms.mu.Lock()
	built := append([]string(nil), ms.built...)
	ms.mu.Unlock()
	require.Equal(t, []string{text}, built, "extraction resolves only the per-KB text model")
	for i, id := range ids {
		memory, err := svc.repo.GetByID(context.Background(), "1", id)
		require.NoError(t, err)
		t.Logf("memory %d tags=%v", i+1, []string(memory.Tags))
		require.NotEmpty(t, memory.Tags, "the real model's entities were parsed and stored")
		joined := strings.ToLower(strings.Join(memory.Tags, "|"))
		if i == 0 {
			require.Contains(t, joined, "postgresql")
		} else {
			require.Contains(t, joined, "kubernetes")
		}
	}
}
