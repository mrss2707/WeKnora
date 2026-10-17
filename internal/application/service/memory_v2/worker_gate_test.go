package memory_v2

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGateTestService(repo *serviceLifecycleRepoFake, mutations bool) *MemoryServiceV2Impl {
	cfg := types.DefaultMemoryV2Config()
	cfg.WorkerMutations = mutations
	return NewMemoryServiceV2(repo, &serviceLifecycleModelServiceFake{
		models:    memoryLifecycleModels(),
		embedder:  &mockEmbedder{},
		chatModel: &mockChat{},
	}, cfg, []string{"tenant-1"})
}

func TestWorkerGate_DefaultIsArmedWithSafeWorkers(t *testing.T) {
	cfg := types.DefaultMemoryV2Config()
	assert.True(t, cfg.WorkerMutations, "scoped, audited workers run by default; the flag is a kill switch")
	assert.True(t, cfg.Dreamer.Enabled, "the dreamer is on by default")
	assert.False(t, cfg.EntityExtractorEnabled, "the entity extractor stays opt-in")
}

func TestWorkerGate_DisarmedStartsNoMutatingWorker(t *testing.T) {
	repo := &serviceLifecycleRepoFake{scopes: []types.MemoryScope{{TenantID: "1", KbID: "kb-1"}}}
	svc := newGateTestService(repo, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.StartWorkers(ctx)
	// The consolidator recomputes hub scores immediately when it starts.
	time.Sleep(50 * time.Millisecond)
	cancel()
	svc.Cleanup()

	assert.Zero(t, repo.hubScoreCallCount(), "consolidator must not run while disarmed")
	require.NotNil(t, svc.dreamer)
	assert.True(t, svc.dreamerConfig().DryRun, "manual dream passes are forced to dry-run")
}

func TestWorkerGate_ArmedStartsMutatingWorkers(t *testing.T) {
	repo := &serviceLifecycleRepoFake{scopes: []types.MemoryScope{{TenantID: "1", KbID: "kb-1"}}}
	svc := newGateTestService(repo, true)
	ctx, cancel := context.WithCancel(context.Background())

	svc.StartWorkers(ctx)
	require.Eventually(t, func() bool { return repo.hubScoreCallCount() > 0 }, time.Second, 10*time.Millisecond,
		"consolidator runs once on start when armed")
	cancel()
	svc.Cleanup()

	assert.Equal(t, svc.config.Dreamer.DryRun, svc.dreamerConfig().DryRun, "armed keeps the configured dry-run")
}
