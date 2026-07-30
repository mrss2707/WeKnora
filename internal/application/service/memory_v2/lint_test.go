package memory_v2

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type lintRepo struct {
	cosineResults [][]*types.MemorySearchResult
	cosineErrs    []error
	cosineCalls   int
	filters       []*types.MemoryFilter
	limits        []int
	embeddings    [][]float32
}

var _ interfaces.MemoryRepositoryV2 = (*lintRepo)(nil)

func (r *lintRepo) Create(context.Context, *types.AgentMemory) error { return nil }
func (r *lintRepo) GetByID(context.Context, string, string) (*types.AgentMemory, error) {
	return nil, nil
}
func (r *lintRepo) GetByFingerprint(context.Context, string, string) (*types.AgentMemory, error) {
	return nil, nil
}
func (r *lintRepo) Update(context.Context, *types.AgentMemory) error { return nil }
func (r *lintRepo) Delete(context.Context, string, string) error     { return nil }
func (r *lintRepo) CreateRelation(context.Context, *types.MemoryRelation) error {
	return nil
}
func (r *lintRepo) GetRelations(context.Context, string, string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (r *lintRepo) DeleteRelation(context.Context, string, string) error { return nil }
func (r *lintRepo) Search(context.Context, *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	return nil, 0, nil
}
func (r *lintRepo) CosineSearch(_ context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	r.cosineCalls++
	r.filters = append(r.filters, cloneLintMemoryFilter(filter))
	r.embeddings = append(r.embeddings, append([]float32(nil), embedding...))
	r.limits = append(r.limits, limit)
	idx := r.cosineCalls - 1
	if idx < len(r.cosineErrs) && r.cosineErrs[idx] != nil {
		return nil, r.cosineErrs[idx]
	}
	if idx < len(r.cosineResults) {
		return r.cosineResults[idx], nil
	}
	return nil, nil
}
func (r *lintRepo) TryDreamerLock(context.Context, string, string) (bool, error) { return false, nil }
func (r *lintRepo) UnlockDreamer(context.Context, string) error                  { return nil }
func (r *lintRepo) ComputeHubScores(context.Context, string) error               { return nil }
func (r *lintRepo) HardDeleteExpired(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *lintRepo) InvalidateResultCache(context.Context, string)              {}
func (r *lintRepo) SetCacheInvalidator(interfaces.CacheInvalidator)            {}
func (r *lintRepo) GetEmbeddingDimension(context.Context, string) (int, error) { return 0, nil }

func cloneLintMemoryFilter(filter *types.MemoryFilter) *types.MemoryFilter {
	if filter == nil {
		return nil
	}
	copy := *filter
	if filter.Tier != nil {
		tier := *filter.Tier
		copy.Tier = &tier
	}
	if filter.Verdicts != nil {
		copy.Verdicts = append([]types.MemoryVerdict(nil), filter.Verdicts...)
	}
	return &copy
}

func lintTestMemory() *types.AgentMemory {
	now := time.Now()
	return &types.AgentMemory{
		ID:         "mem-1",
		TenantID:   "tenant-1",
		Content:    "memory content",
		Verdict:    types.VerdictNone,
		CreatedAt:  now,
		UpdatedAt:  now,
		Importance: 2,
		HubScore:   1,
		Tags:       types.TagsArray{"tag"},
	}
}

func TestRunLintOnWrite_DisabledDoesNotCallRepo(t *testing.T) {
	repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{{Memory: &types.AgentMemory{ID: "other"}, Score: 1}}}}
	issues := RunLintOnWrite(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{Enabled: false}, []float32{0.1})

	assert.Nil(t, issues)
	assert.Zero(t, repo.cosineCalls)
}

func TestRunLintOnWrite_AggregatesRules(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	memory := lintTestMemory()
	memory.Tags = nil
	memory.HubScore = 0
	memory.Importance = 0
	memory.CreatedAt = old
	memory.UpdatedAt = old
	memory.Verdict = types.VerdictWIP
	repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{
		{{Memory: &types.AgentMemory{ID: "decision", Verdict: types.VerdictDecision}, Score: 0.9}},
		{{Memory: &types.AgentMemory{ID: "dupe", Verdict: types.VerdictNone}, Score: 0.98}},
	}}

	issues := RunLintOnWrite(context.Background(), memory, repo, types.LintOnWriteConfig{Enabled: true}, []float32{0.1})

	assert.Len(t, issues, 6)
	assert.Equal(t, []string{"orphans", "staleness", "contradiction", "duplication", "graph_fragmentation", "verdict_consistency"}, lintIssueRules(issues))
	assert.Equal(t, 2, repo.cosineCalls)
}

func lintIssueRules(issues []types.MemoryLintIssue) []string {
	rules := make([]string, len(issues))
	for i, issue := range issues {
		rules[i] = issue.Rule
	}
	return rules
}

func TestLintOrphans(t *testing.T) {
	memory := lintTestMemory()
	memory.Tags = nil
	memory.HubScore = 0

	issues := lintOrphans(context.Background(), memory, nil)
	require.Len(t, issues, 1)
	assert.Equal(t, "warning", issues[0].Severity)

	memory.Tags = types.TagsArray{"tag"}
	assert.Nil(t, lintOrphans(context.Background(), memory, nil))

	memory.Tags = nil
	memory.HubScore = 1
	assert.Nil(t, lintOrphans(context.Background(), memory, nil))
}

func TestLintStaleness(t *testing.T) {
	memory := lintTestMemory()
	memory.CreatedAt = time.Now().Add(-91 * 24 * time.Hour)
	memory.Importance = 0

	issues := lintStaleness(context.Background(), memory, types.LintOnWriteConfig{})
	require.Len(t, issues, 1)
	assert.Equal(t, "info", issues[0].Severity)

	memory.CreatedAt = time.Now().Add(-2 * 24 * time.Hour)
	assert.Nil(t, lintStaleness(context.Background(), memory, types.LintOnWriteConfig{StaleThresholdDays: 90}))

	memory.CreatedAt = time.Now().Add(-100 * 24 * time.Hour)
	memory.Importance = 1
	assert.Nil(t, lintStaleness(context.Background(), memory, types.LintOnWriteConfig{}))
}

func TestLintContradiction(t *testing.T) {
	t.Run("empty embedding", func(t *testing.T) {
		repo := &lintRepo{}
		issues := lintContradiction(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{}, nil)
		assert.Nil(t, issues)
		assert.Zero(t, repo.cosineCalls)
	})

	t.Run("repo error advisory", func(t *testing.T) {
		repo := &lintRepo{cosineErrs: []error{errors.New("vector down")}}
		issues := lintContradiction(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{}, []float32{0.1})
		assert.Nil(t, issues)
		assert.Equal(t, 1, repo.cosineCalls)
	})

	t.Run("skips nil self and low score", func(t *testing.T) {
		memory := lintTestMemory()
		memory.Verdict = types.VerdictFixed
		repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{
			{Memory: nil, Score: 1},
			{Memory: &types.AgentMemory{ID: memory.ID, Verdict: types.VerdictRefuted}, Score: 1},
			{Memory: &types.AgentMemory{ID: "low", Verdict: types.VerdictRefuted}, Score: 0.84},
		}}}
		issues := lintContradiction(context.Background(), memory, repo, types.LintOnWriteConfig{}, []float32{0.1})
		assert.Nil(t, issues)
		assert.Equal(t, "tenant-1", repo.filters[0].TenantID)
		assert.Equal(t, 5, repo.filters[0].Limit)
		assert.Equal(t, 5, repo.limits[0])
	})

	t.Run("fixed versus refuted", func(t *testing.T) {
		memory := lintTestMemory()
		memory.Verdict = types.VerdictFixed
		repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{
			{Memory: &types.AgentMemory{ID: "refuted", Verdict: types.VerdictRefuted}, Score: 0.85},
		}}}
		issues := lintContradiction(context.Background(), memory, repo, types.LintOnWriteConfig{}, []float32{0.1})
		require.Len(t, issues, 1)
		assert.Equal(t, "critical", issues[0].Severity)
		assert.Equal(t, "refuted", issues[0].SourceID)
	})

	t.Run("wip versus decision", func(t *testing.T) {
		memory := lintTestMemory()
		memory.Verdict = types.VerdictWIP
		repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{
			{Memory: &types.AgentMemory{ID: "decision", Verdict: types.VerdictDecision}, Score: 0.9},
		}}}
		issues := lintContradiction(context.Background(), memory, repo, types.LintOnWriteConfig{ContradictionThreshold: 0.9}, []float32{0.1})
		require.Len(t, issues, 1)
		assert.Equal(t, "decision", issues[0].SourceID)
	})
}

func TestLintDuplication(t *testing.T) {
	t.Run("empty embedding", func(t *testing.T) {
		repo := &lintRepo{}
		issues := lintDuplication(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{}, nil)
		assert.Nil(t, issues)
		assert.Zero(t, repo.cosineCalls)
	})

	t.Run("repo error advisory", func(t *testing.T) {
		repo := &lintRepo{cosineErrs: []error{errors.New("vector down")}}
		issues := lintDuplication(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{}, []float32{0.1})
		assert.Nil(t, issues)
		assert.Equal(t, 1, repo.cosineCalls)
	})

	t.Run("skips nil self below threshold", func(t *testing.T) {
		memory := lintTestMemory()
		repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{
			{Memory: nil, Score: 1},
			{Memory: &types.AgentMemory{ID: memory.ID}, Score: 1},
			{Memory: &types.AgentMemory{ID: "below"}, Score: 0.949},
		}}}
		issues := lintDuplication(context.Background(), memory, repo, types.LintOnWriteConfig{}, []float32{0.1})
		assert.Nil(t, issues)
		assert.Equal(t, "tenant-1", repo.filters[0].TenantID)
		assert.Equal(t, 3, repo.filters[0].Limit)
		assert.Equal(t, 3, repo.limits[0])
	})

	t.Run("score at threshold warns", func(t *testing.T) {
		repo := &lintRepo{cosineResults: [][]*types.MemorySearchResult{{
			{Memory: &types.AgentMemory{ID: "dupe"}, Score: 0.95},
		}}}
		issues := lintDuplication(context.Background(), lintTestMemory(), repo, types.LintOnWriteConfig{}, []float32{0.1})
		require.Len(t, issues, 1)
		assert.Equal(t, "warning", issues[0].Severity)
		assert.Equal(t, "dupe", issues[0].SourceID)
	})
}

func TestLintGraphFragmentation(t *testing.T) {
	memory := lintTestMemory()
	memory.HubScore = 0
	memory.Importance = 1
	issues := lintGraphFragmentation(context.Background(), memory)
	require.Len(t, issues, 1)
	assert.Equal(t, "info", issues[0].Severity)

	memory.Importance = 2
	assert.Nil(t, lintGraphFragmentation(context.Background(), memory))

	memory.Importance = 1
	memory.HubScore = 1
	assert.Nil(t, lintGraphFragmentation(context.Background(), memory))

	memory.HubScore = 0
	memory.Importance = -2
	assert.Nil(t, lintGraphFragmentation(context.Background(), memory))
}

func TestLintVerdictConsistency(t *testing.T) {
	memory := lintTestMemory()
	memory.Verdict = types.VerdictWIP
	memory.UpdatedAt = time.Now().Add(-31 * 24 * time.Hour)
	issues := lintVerdictConsistency(context.Background(), memory)
	require.Len(t, issues, 1)
	assert.Equal(t, "warning", issues[0].Severity)

	memory.UpdatedAt = time.Now().Add(-30 * 24 * time.Hour)
	assert.Nil(t, lintVerdictConsistency(context.Background(), memory))

	memory.UpdatedAt = time.Now().Add(-1 * time.Hour)
	assert.Nil(t, lintVerdictConsistency(context.Background(), memory))

	memory.Verdict = types.VerdictDecision
	memory.UpdatedAt = time.Now().Add(-60 * 24 * time.Hour)
	assert.Nil(t, lintVerdictConsistency(context.Background(), memory))
}
