package memory_v2

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func enabledMemoryConfig() types.MemoryV2Config { return types.DefaultMemoryV2Config() }

func disabledMemoryConfig() types.MemoryV2Config {
	c := types.DefaultMemoryV2Config()
	c.Enabled = false
	return c
}

func TestMemoryV2Readiness_States(t *testing.T) {
	t.Run("nil service", func(t *testing.T) {
		var svc *MemoryServiceV2Impl
		r := svc.Readiness()
		assert.False(t, r.Ready)
		assert.Equal(t, types.MemoryV2ReasonRepoUnavailable, r.Reason)
	})

	t.Run("nil repository", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{config: enabledMemoryConfig()}
		r := svc.Readiness()
		assert.False(t, r.Ready)
		assert.Equal(t, types.MemoryV2ReasonRepoUnavailable, r.Reason)
	})

	t.Run("enabled default", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: enabledMemoryConfig()}
		r := svc.Readiness()
		assert.True(t, r.Ready)
		assert.Equal(t, types.MemoryV2ReasonEnabled, r.Reason)
	})

	t.Run("config disabled", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: disabledMemoryConfig()}
		r := svc.Readiness()
		assert.False(t, r.Ready)
		assert.Equal(t, types.MemoryV2ReasonConfigDisabled, r.Reason)
	})

	t.Run("lite override wins over config", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: enabledMemoryConfig()}
		svc.SetReadinessReason(types.MemoryV2ReasonLiteMode)
		r := svc.Readiness()
		assert.False(t, r.Ready)
		assert.Equal(t, types.MemoryV2ReasonLiteMode, r.Reason)
	})
}

func TestMemoryV2StartWorkers_NotReadyStartsNothing(t *testing.T) {
	t.Run("disabled config", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: disabledMemoryConfig()}
		svc.StartWorkers(context.Background())
		assert.Nil(t, svc.cancel, "workers must not start when config disabled")
		svc.Cleanup() // must not block or panic
	})

	t.Run("lite mode", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: enabledMemoryConfig()}
		svc.SetReadinessReason(types.MemoryV2ReasonLiteMode)
		svc.StartWorkers(context.Background())
		assert.Nil(t, svc.cancel, "workers must not start in Lite mode")
		svc.Cleanup()
	})
}

func TestMemoryV2Cleanup_ConcurrentAndIdempotent(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	svc := NewMemoryServiceV2(repo, nil, enabledMemoryConfig(), nil)
	svc.StartWorkers(context.Background())
	require.NotNil(t, svc.cancel, "workers must start when ready")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.Cleanup()
		}()
	}
	wg.Wait()
	// Cleanup stays a no-op after workers stopped (registered hooks may call
	// it again during shutdown).
	svc.Cleanup()
}

func TestMemoryV2SaveMemory_ScopeAndReadiness(t *testing.T) {
	ctx := context.Background()

	t.Run("empty tenant rejected before repository touch", func(t *testing.T) {
		repo := &serviceLifecycleRepoFake{}
		svc := &MemoryServiceV2Impl{repo: repo, config: enabledMemoryConfig()}
		memory := &types.AgentMemory{TenantID: "", Content: "a valid memory content about the project"}
		_, err := svc.SaveMemory(ctx, memory)
		var validationErr *ErrMemoryValidation
		require.ErrorAs(t, err, &validationErr)
		assert.Contains(t, err.Error(), "tenant_id")
		repo.mu.Lock()
		defer repo.mu.Unlock()
		assert.Empty(t, repo.created)
	})

	t.Run("not ready rejects with concrete reason", func(t *testing.T) {
		repo := &serviceLifecycleRepoFake{}
		svc := &MemoryServiceV2Impl{repo: repo, config: disabledMemoryConfig()}
		memory := &types.AgentMemory{TenantID: "tenant-A", Content: "a valid memory content about the project"}
		_, err := svc.SaveMemory(ctx, memory)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ready")
		assert.Contains(t, err.Error(), types.MemoryV2ReasonConfigDisabled)
		repo.mu.Lock()
		defer repo.mu.Unlock()
		assert.Empty(t, repo.created)
	})

	t.Run("tenant scope is propagated to the repository", func(t *testing.T) {
		repo := &serviceLifecycleRepoFake{}
		modelSvc := &serviceLifecycleModelServiceFake{
			models:   memoryLifecycleModels(),
			embedder: &mockEmbedder{},
		}
		svc := NewMemoryServiceV2(repo, modelSvc, enabledMemoryConfig(), nil)

		_, err := svc.SaveMemory(ctx, &types.AgentMemory{
			TenantID: "tenant-A",
			Content:  "The project deploys on Wednesday mornings",
		})
		require.NoError(t, err)
		_, err = svc.SaveMemory(ctx, &types.AgentMemory{
			TenantID: "tenant-B",
			Content:  "A completely different memory for another workspace",
		})
		require.NoError(t, err)

		repo.mu.Lock()
		defer repo.mu.Unlock()
		require.Len(t, repo.created, 2)
		assert.Equal(t, "tenant-A", repo.created[0].TenantID)
		assert.Equal(t, "tenant-B", repo.created[1].TenantID)
		assert.NotEqual(t, repo.created[0].TenantID, repo.created[1].TenantID)
	})
}

func TestMemoryV2AddEpisode_EmptyTenantNoOp(t *testing.T) {
	repo := &serviceLifecycleRepoFake{}
	svc := &MemoryServiceV2Impl{repo: repo, config: enabledMemoryConfig()}
	err := svc.AddEpisode(context.Background(), "", "user-1", "session-1", []types.Message{
		{Role: "user", Content: "hello world message"},
	})
	require.NoError(t, err)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Empty(t, repo.created)
}

func TestMemoryV2ConsolidateDream_NotReadyOrEmptyTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("not ready returns empty pass", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: disabledMemoryConfig()}
		result, err := svc.ConsolidateDream(ctx, "tenant-A")
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("empty tenant returns empty pass", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: enabledMemoryConfig()}
		result, err := svc.ConsolidateDream(ctx, "   ")
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestMemoryV2AssessHealth_NotReadyOrEmptyTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("not ready errors", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: disabledMemoryConfig()}
		_, err := svc.AssessHealth(ctx, "tenant-A", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), types.MemoryV2ReasonConfigDisabled)
	})

	t.Run("empty tenant errors", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{repo: &serviceLifecycleRepoFake{}, config: enabledMemoryConfig()}
		_, err := svc.AssessHealth(ctx, "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})
}

func TestMemoryV2SearchMemories_NilRepositoryNotReady(t *testing.T) {
	svc := &MemoryServiceV2Impl{config: enabledMemoryConfig()}
	_, err := svc.SearchMemories(context.Background(), "query", &types.MemoryFilter{TenantID: "tenant-A"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}