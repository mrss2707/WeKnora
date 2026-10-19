package memory_v2

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealth_ReportsUnusableDreamerModel(t *testing.T) {
	r, ms, store := newPurposeFixture()
	svc := NewMemoryServiceV2(settingsRepo{&serviceLifecycleRepoFake{}, store}, ms, enabledMemoryConfig(), nil)
	svc.resolver = r
	ctx := context.Background()
	fresh := func() *types.HealthReport {
		return &types.HealthReport{TenantID: "1", BySeverity: map[string]int{}, Checks: map[string]types.MemoryHealthCheck{}}
	}

	report := fresh()
	svc.checkDreamerModel(ctx, report, "1", "kb-1")
	assert.Empty(t, report.Issues, "the KB summary model is usable")
	assert.Equal(t, "completed", report.Checks["dreamer_model"].Status)

	store.rows["1/kb-1"] = &types.MemoryV2ScopeSettings{TenantID: "1", KbID: "kb-1", DreamerModelID: "deleted"}
	report = fresh()
	svc.checkDreamerModel(ctx, report, "1", "kb-1")
	require.Len(t, report.Issues, 1)
	assert.Equal(t, dreamerModelIssue, report.Issues[0].Type)
	assert.Equal(t, "high", report.Issues[0].Severity)
	assert.Equal(t, 1, report.BySeverity["high"])
	assert.Equal(t, 1, report.TotalIssues)

	report = fresh()
	svc.checkDreamerModel(ctx, report, "1", "kb-1-nm")
	assert.Empty(t, report.Issues, "settings are per knowledge base")

	svc.config.Dreamer.Enabled = false
	report = fresh()
	svc.checkDreamerModel(ctx, report, "1", "kb-1")
	assert.Empty(t, report.Issues, "a disabled Dreamer needs no model")
	assert.NotContains(t, report.Checks, "dreamer_model")
}
