package workers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockHealthCheckerRepo implements interfaces.MemoryRepositoryV2 for testing the health checker.
type mockHealthCheckerRepo struct {
	mu                 sync.Mutex
	createFunc         func(ctx context.Context, memory *types.AgentMemory) error
	getByIDFunc        func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error)
	updateFunc         func(ctx context.Context, memory *types.AgentMemory) error
	deleteFunc         func(ctx context.Context, tenantID, id string) error
	searchFunc         func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error)
	cosineSearchFunc   func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)
	tryDreamerLockFunc func(ctx context.Context, tenantID string, workerID string) (bool, error)
	unlockDreamerFunc  func(ctx context.Context, tenantID string) error
	computeHubScores   func(ctx context.Context, tenantID string) error
	invalidateCache    func(ctx context.Context, tenantID string)

	// Tracking
	searchCalls int
}

func (m *mockHealthCheckerRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockHealthCheckerRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockHealthCheckerRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockHealthCheckerRepo) Delete(ctx context.Context, tenantID, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockHealthCheckerRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	m.mu.Lock()
	m.searchCalls++
	m.mu.Unlock()
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockHealthCheckerRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockHealthCheckerRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockHealthCheckerRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockHealthCheckerRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockHealthCheckerRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}

func (m *mockHealthCheckerRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockHealthCheckerRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockHealthCheckerRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockHealthCheckerRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockHealthCheckerRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockHealthCheckerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeHealthCheckerMemory(id, tenantID, content string, tags []string, importance int, hubScore float64, verdict types.MemoryVerdict, createdAt, updatedAt time.Time) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Tags:       tags,
		Importance: importance,
		HubScore:   hubScore,
		Verdict:    verdict,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		MemoryType: "semantic",
		Tier:       2,
	}
}

func hcSearchResult(mem *types.AgentMemory) *types.MemorySearchResult {
	return &types.MemorySearchResult{Memory: mem, Score: 1.0}
}

func newTestHealthChecker(repo *mockHealthCheckerRepo) *HealthChecker {
	return &HealthChecker{repo: repo}
}

// freshMemory creates a memory with recent timestamps that will not trigger stale or verdict checks.
func freshMemory(id, tenantID, content string, tags []string, importance int, hubScore float64) *types.AgentMemory {
	return makeHealthCheckerMemory(id, tenantID, content, tags, importance, hubScore, types.VerdictNone, time.Now(), time.Now())
}

// ---------------------------------------------------------------------------
// Check 1: Orphans — memories with 0 tags AND hub_score=0
// ---------------------------------------------------------------------------

func TestHealthChecker_Orphans_Detected(t *testing.T) {
	orphan := freshMemory("orphan-1", "tenant-1", "Isolated memory", nil, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(orphan)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "orphan", report.Issues[0].Type)
	assert.Equal(t, "orphan-1", report.Issues[0].MemoryID)
	assert.Equal(t, "medium", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Description, "no tags")
	assert.Contains(t, report.Issues[0].Suggestion, "Add relevant tags")
}

func TestHealthChecker_Orphans_WithTagsNotOrphan(t *testing.T) {
	notOrphan := freshMemory("normal-1", "tenant-1", "Has tags memory", []string{"tag1", "tag2"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(notOrphan)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "memory with tags should not be an orphan")
}

func TestHealthChecker_Orphans_WithHubScoreNotOrphan(t *testing.T) {
	connected := freshMemory("connected-1", "tenant-1", "Connected memory", nil, 3, 5)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(connected)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "memory with hub_score != 0 should not be an orphan")
}

func TestHealthChecker_Orphans_MixedMemories(t *testing.T) {
	orphan := freshMemory("orphan-1", "tenant-1", "Orphan", nil, 3, 0)
	normal := freshMemory("normal-1", "tenant-1", "Normal", []string{"tag1"}, 3, 5)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(orphan), hcSearchResult(normal)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "orphan-1", report.Issues[0].MemoryID, "only the orphan should be flagged")
}

// ---------------------------------------------------------------------------
// Check 2: Stale facts — >180d old, importance < 1
// ---------------------------------------------------------------------------

func TestHealthChecker_StaleFacts_Detected(t *testing.T) {
	// hub_score=0.5 to avoid fragmentation isolation (hub_score != 0)
	old := makeHealthCheckerMemory("stale-1", "tenant-1", "Old stale fact",
		[]string{"fact"}, 0, 0.5, types.VerdictNone,
		time.Now().Add(-200*24*time.Hour),
		time.Now().Add(-200*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(old)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "stale", report.Issues[0].Type)
	assert.Equal(t, "stale-1", report.Issues[0].MemoryID)
	assert.Equal(t, "low", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Suggestion, "pruning")
}

func TestHealthChecker_StaleFacts_RecentMemoryNotStale(t *testing.T) {
	recent := makeHealthCheckerMemory("recent-1", "tenant-1", "Recent memory",
		[]string{"fact"}, 0, 0.5, types.VerdictNone,
		time.Now().Add(-30*24*time.Hour),
		time.Now().Add(-30*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(recent)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "recent memory should not be stale")
}

func TestHealthChecker_StaleFacts_HighImportanceNotStale(t *testing.T) {
	oldImportant := makeHealthCheckerMemory("old-imp-1", "tenant-1", "Old but important",
		[]string{"fact"}, 5, 0, types.VerdictNone,
		time.Now().Add(-200*24*time.Hour),
		time.Now().Add(-200*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(oldImportant)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "old memory with high importance should not be stale")
}

func TestHealthChecker_StaleFacts_BoundaryExactly180Days(t *testing.T) {
	// Code uses > 180*24 hours, so exactly 180 days should NOT trigger
	// Code uses > 180*24 hours. Add a small buffer to avoid the stale check
	// firing due to the delay between two time.Now() calls (memory creation vs check).
	exactly180 := makeHealthCheckerMemory("boundary-1", "tenant-1", "Exactly 180 days",
		[]string{"fact"}, 0, 0.5, types.VerdictNone,
		time.Now().Add(-180*24*time.Hour+time.Second), // slightly less than 180d
		time.Now().Add(-180*24*time.Hour+time.Second),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(exactly180)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "exactly 180 days should NOT trigger stale (strictly >)")
}

func TestHealthChecker_StaleFacts_AllowPruningSuggestion(t *testing.T) {
	old := makeHealthCheckerMemory("stale-2", "tenant-1", "Another stale fact",
		[]string{"fact"}, -1, 0.5, types.VerdictNone,
		time.Now().Add(-300*24*time.Hour),
		time.Now().Add(-300*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(old)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Contains(t, report.Issues[0].Description, "300 days")
	assert.Contains(t, report.Issues[0].Description, "importance (-1)")
}

// ---------------------------------------------------------------------------
// Check 3: Contradictions — negation in one but not the other
//
// The contradiction check pairs each memory i with the first j > i where
// negation status differs. This means with [no, yes] we get 1 contradiction.
// With [no, yes, no] we get 2 (i0-j1, i1-j2).
// ---------------------------------------------------------------------------

func TestHealthChecker_Contradictions_Detected(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The sky is blue", []string{"sky"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "The sky is not blue", []string{"sky"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "contradiction", report.Issues[0].Type)
	assert.Equal(t, "mem-a", report.Issues[0].MemoryID)
	assert.Equal(t, "high", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Description, "mem-a")
	assert.Contains(t, report.Issues[0].Description, "mem-b")
}

func TestHealthChecker_Contradictions_BothSameNegationNoIssue(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The sky is not blue", []string{"sky"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "It is not green", []string{"sky"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "both memories with negation should not contradict")
}

func TestHealthChecker_Contradictions_NeitherHasNegation(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The sky is blue", []string{"sky"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "The grass is green", []string{"grass"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "no negation in either memory should not trigger contradiction")
}

func TestHealthChecker_Contradictions_DifferentTenantsNotCompared(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The sky is blue", []string{"sky"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-2", "The sky is not blue", []string{"sky"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			// Mock returns cross-tenant data, but AssessHealth passes tenant-1 in the filter
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	// The contradiction check filters by TenantID before comparing negations.
	// mem-a(tenant-1) vs mem-b(tenant-2): a.TenantID != b.TenantID → continue
	// So no contradiction is detected.
	assert.Empty(t, report.Issues, "different tenant memories should not be compared")
}

// ---------------------------------------------------------------------------
// Check 4: Duplications — content prefix collision (first 20 chars)
// ---------------------------------------------------------------------------

func TestHealthChecker_Duplications_Detected(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The quick brown fox jumps over the lazy dog", []string{"fox"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "The quick brown fox jumps over the sleeping cat", []string{"fox"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "duplication", report.Issues[0].Type)
	assert.Equal(t, "mem-b", report.Issues[0].MemoryID)
	assert.Equal(t, "medium", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Description, "mem-a")
	assert.Contains(t, report.Issues[0].Suggestion, "merge")
}

func TestHealthChecker_Duplications_DifferentPrefixNotDuplicate(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The quick brown fox jumps over the lazy dog", []string{"fox"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "A completely different memory content here", []string{"other"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "different first 20 chars should not be duplicates")
}

func TestHealthChecker_Duplications_ShortContentSkipped(t *testing.T) {
	short := freshMemory("short-1", "tenant-1", "Short", []string{"tiny"}, 3, 0)
	// len("Short") = 5 < 20, should be skipped

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(short)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "short content (<20 chars) should be skipped")
}

func TestHealthChecker_Duplications_ThreeWithSamePrefix(t *testing.T) {
	memA := freshMemory("mem-a", "tenant-1", "The quick brown fox jumps over", []string{"fox"}, 3, 0)
	memB := freshMemory("mem-b", "tenant-1", "The quick brown fox jumps high", []string{"fox"}, 3, 0)
	memC := freshMemory("mem-c", "tenant-1", "The quick brown fox jumps away", []string{"fox"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(memA), hcSearchResult(memB), hcSearchResult(memC)}, 3, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	// mem-a is the first, so mem-b and mem-c are flagged as duplicates of mem-a
	require.Len(t, report.Issues, 2)
	assert.Equal(t, "mem-b", report.Issues[0].MemoryID)
	assert.Equal(t, "mem-c", report.Issues[1].MemoryID)
	for _, issue := range report.Issues {
		assert.Equal(t, "duplication", issue.Type)
		assert.Contains(t, issue.Description, "mem-a")
	}
}

// ---------------------------------------------------------------------------
// Check 5: Graph fragmentation — isolated node ratio > 30%
//
// Isolated = hub_score==0 AND |importance| < 2.
// All fragmentation-test memories have a tag to avoid orphan detection.
// ---------------------------------------------------------------------------

func TestHealthChecker_GraphFragmentation_HighRatioDetected(t *testing.T) {
	// 2 isolated (hub_score=0, |importance|<2), 1 connected = 66.6% > 30%
	isolated1 := freshMemory("iso-1", "tenant-1", "Isolated one", []string{"tag1"}, 1, 0)
	isolated2 := freshMemory("iso-2", "tenant-1", "Isolated two", []string{"tag1"}, 0, 0)
	connected := freshMemory("conn-1", "tenant-1", "Connected one", []string{"tag1"}, 3, 5)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				hcSearchResult(isolated1), hcSearchResult(isolated2), hcSearchResult(connected),
			}, 3, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	// Only graph_fragmentation should fire (66.6% > 30%)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "graph_fragmentation", report.Issues[0].Type)
	assert.Empty(t, report.Issues[0].MemoryID, "graph fragmentation has no memory ID")
	assert.Equal(t, "high", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Description, "67%")
	assert.Contains(t, report.Issues[0].Suggestion, "auto-linker")
}

func TestHealthChecker_GraphFragmentation_BelowThresholdNoIssue(t *testing.T) {
	// 1 isolated, 3 connected = 25% <= 30%, no issue
	isolated := freshMemory("iso-1", "tenant-1", "Isolated", []string{"tag1"}, 1, 0)
	connected1 := freshMemory("conn-1", "tenant-1", "Connected one", []string{"tag1"}, 3, 5)
	connected2 := freshMemory("conn-2", "tenant-1", "Connected two", []string{"tag1"}, 3, 5)
	connected3 := freshMemory("conn-3", "tenant-1", "Connected three", []string{"tag1"}, 3, 5)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				hcSearchResult(isolated), hcSearchResult(connected1),
				hcSearchResult(connected2), hcSearchResult(connected3),
			}, 4, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "25% fragmentation should not trigger")
}

func TestHealthChecker_GraphFragmentation_Exactly30Percent(t *testing.T) {
	// 3 isolated, 7 connected = 30% exactly. Code uses > 30, so no issue.
	all := make([]*types.MemorySearchResult, 0, 10)
	for i := 0; i < 3; i++ {
		m := freshMemory(fmt.Sprintf("iso-%d", i), "tenant-1", "Isolated", []string{"tag1"}, 0, 0)
		all = append(all, hcSearchResult(m))
	}
	for i := 0; i < 7; i++ {
		m := freshMemory(fmt.Sprintf("conn-%d", i), "tenant-1", "Connected", []string{"tag1"}, 3, 5)
		all = append(all, hcSearchResult(m))
	}

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return all, 10, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "exactly 30% should NOT trigger (strictly >)")
}

func TestHealthChecker_GraphFragmentation_ImportanceBoundary(t *testing.T) {
	// |importance| < 2 means importance is -1, 0, or 1
	// importance=2: |2| < 2 is false, so NOT isolated even with hub_score=0
	imp2 := freshMemory("imp2-1", "tenant-1", "Importance 2", []string{"tag1"}, 2, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(imp2)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "importance=2 with hub_score=0 should not be isolated")
}

// ---------------------------------------------------------------------------
// Check 6: Verdict consistency — WIP memories unchanged for >30 days
//
// All verdict-test memories have a tag to avoid orphan detection.
// ---------------------------------------------------------------------------

func TestHealthChecker_VerdictConsistency_WipOldDetected(t *testing.T) {
	wipOld := makeHealthCheckerMemory("wip-1", "tenant-1", "WIP that never finished",
		[]string{"tag1"}, 3, 0, types.VerdictWIP,
		time.Now().Add(-60*24*time.Hour),
		time.Now().Add(-45*24*time.Hour), // updated 45 days ago (>30 days)
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(wipOld)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, "verdict_consistency", report.Issues[0].Type)
	assert.Equal(t, "wip-1", report.Issues[0].MemoryID)
	assert.Equal(t, "warning", report.Issues[0].Severity)
	assert.Contains(t, report.Issues[0].Description, "45 days")
	assert.Contains(t, report.Issues[0].Suggestion, "WIP status")
}

func TestHealthChecker_VerdictConsistency_WipRecentNoIssue(t *testing.T) {
	wipRecent := makeHealthCheckerMemory("wip-2", "tenant-1", "Active WIP",
		[]string{"tag1"}, 3, 0, types.VerdictWIP,
		time.Now().Add(-10*24*time.Hour),
		time.Now().Add(-5*24*time.Hour), // updated 5 days ago (<30 days)
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(wipRecent)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "recently updated WIP should not be flagged")
}

func TestHealthChecker_VerdictConsistency_NonWipNotFlagged(t *testing.T) {
	notWip := makeHealthCheckerMemory("done-1", "tenant-1", "Completed memory",
		[]string{"tag1"}, 3, 0, types.VerdictDecision,
		time.Now().Add(-100*24*time.Hour),
		time.Now().Add(-100*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(notWip)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "non-WIP memories should not be flagged")
}

func TestHealthChecker_VerdictConsistency_Exactly30Days(t *testing.T) {
	// Code uses daysSinceUpdate > 30, so exactly 30 should NOT trigger
	wip30 := makeHealthCheckerMemory("wip-30", "tenant-1", "WIP at 30 days",
		[]string{"tag1"}, 3, 0, types.VerdictWIP,
		time.Now().Add(-60*24*time.Hour),
		time.Now().Add(-30*24*time.Hour), // exactly 30 days ago
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(wip30)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Empty(t, report.Issues, "exactly 30 days should NOT trigger (strictly >)")
}

// ---------------------------------------------------------------------------
// HealthReport structure (total issues, by severity, individual issue fields)
//
// Uses 2 memories: orphan (medium) + stale (low). No contradictions (no negation
// words in any content), no duplications (all content is short), no fragmentation
// (1 isolated out of 2 = 50% > 30%, but stale has hub=0 and |importance|=0 < 2,
// so it IS isolated — but we use a single orphan-only memory here to keep it simple).
// ---------------------------------------------------------------------------

func TestHealthChecker_AssessHealth_ReportStructure(t *testing.T) {
	// orphan: no tags, hub=0 → orphan (medium)
	orphan := freshMemory("orphan-1", "tenant-1", "Orphan memory zero tags", nil, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(orphan)}, 1, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)

	assert.Equal(t, "tenant-1", report.TenantID)
	assert.False(t, report.CheckedAt.IsZero(), "CheckedAt should be set")
	assert.Equal(t, 1, report.TotalIssues)
	assert.Equal(t, 1, report.BySeverity["medium"])
	assert.Len(t, report.Issues, 1)

	// Individual issue field completeness
	issue := report.Issues[0]
	assert.NotEmpty(t, issue.Type)
	assert.NotEmpty(t, issue.MemoryID)
	assert.NotEmpty(t, issue.Description)
	assert.NotEmpty(t, issue.Severity)
	assert.NotEmpty(t, issue.Suggestion)
}

func TestHealthChecker_AssessHealth_ReportMultipleSeverities(t *testing.T) {
	// Two memories triggering different checks with different severities:
	// orphan-1: no tags, hub=0 → orphan (medium)
	// stale-1: tags=["fact"], importance=0, hub=0, 200d old → stale (low)
	// No negation in any content, so no contradictions.
	// stale-1 has |importance|=0 < 2 and hub=0 → isolated for fragmentation.
	// 1 isolated / 2 total = 50% > 30% → fragmentation (high)!
	// Total: 3 issues: orphan (medium) + stale (low) + graph_fragmentation (high)
	orphan := freshMemory("orphan-1", "tenant-1", "Orphan memory", nil, 3, 0)
	stale := makeHealthCheckerMemory("stale-1", "tenant-1", "Old fact",
		[]string{"fact"}, 0, 0, types.VerdictNone,
		time.Now().Add(-200*24*time.Hour),
		time.Now().Add(-200*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(orphan), hcSearchResult(stale)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)

	assert.Equal(t, 3, report.TotalIssues)
	assert.Equal(t, 1, report.BySeverity["low"], "stale")
	assert.Equal(t, 1, report.BySeverity["medium"], "orphan")
	assert.Equal(t, 1, report.BySeverity["high"], "fragmentation")

	seenTypes := make(map[string]int)
	for _, issue := range report.Issues {
		seenTypes[issue.Type]++
	}
	assert.Equal(t, 1, seenTypes["orphan"])
	assert.Equal(t, 1, seenTypes["stale"])
	assert.Equal(t, 1, seenTypes["graph_fragmentation"])
}

// ---------------------------------------------------------------------------
// Empty results — no issues found
// ---------------------------------------------------------------------------

func TestHealthChecker_AssessHealth_EmptyResults(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Equal(t, 0, report.TotalIssues)
	assert.Empty(t, report.Issues)
	assert.Empty(t, report.BySeverity)
	assert.Equal(t, "tenant-1", report.TenantID)
	assert.False(t, report.CheckedAt.IsZero())
}

func TestHealthChecker_AssessHealth_NilMemoryInResultsSkipped(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				{Memory: nil, Score: 1.0},
				{Memory: nil, Score: 1.0},
			}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)
	assert.Equal(t, 0, report.TotalIssues, "nil memories should be skipped")
	assert.Empty(t, report.Issues)
}

func TestHealthChecker_runAllChecks_EmptyMemories(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	hc := newTestHealthChecker(repo)

	hc.runChecks(context.Background())
	assert.Equal(t, 1, repo.searchCalls, "search should be called")
}

// ---------------------------------------------------------------------------
// Error resilience — repo search error handled gracefully
// ---------------------------------------------------------------------------

func TestHealthChecker_AssessHealth_SearchErrorReturnsNil(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err, "search error should be handled gracefully")
	assert.Equal(t, 0, report.TotalIssues, "no issues when search fails")
	assert.Empty(t, report.Issues)
}

func TestHealthChecker_runAllChecks_SearchErrorGraceful(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	hc := newTestHealthChecker(repo)

	// Should not panic when search fails
	hc.runChecks(context.Background())
	assert.Equal(t, 1, repo.searchCalls, "search should be attempted")
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestHealthChecker_AssessHealth_CancelledContext(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			default:
				return []*types.MemorySearchResult{
					hcSearchResult(freshMemory("m1", "tenant-1", "test", nil, 3, 0)),
				}, 1, nil
			}
		},
	}
	hc := newTestHealthChecker(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := hc.AssessHealth(ctx, "tenant-1", "")
	// With cancelled context, search returns ctx.Err(). runAllChecks logs and returns nil.
	require.NoError(t, err, "should handle cancelled context gracefully")
	assert.Equal(t, 0, report.TotalIssues)
}

func TestHealthChecker_Run_PreCancelledContext(t *testing.T) {
	repo := &mockHealthCheckerRepo{}
	hc := newTestHealthChecker(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		hc.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after pre-cancelled context")
	}
}

func TestHealthChecker_Run_ContextCancellation(t *testing.T) {
	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	hc := newTestHealthChecker(repo)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hc.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// Run returned cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5 seconds after context cancellation")
	}

	assert.Equal(t, 1, repo.searchCalls, "initial check should run before cancellation")
}

// ---------------------------------------------------------------------------
// containsNegation unit tests
// ---------------------------------------------------------------------------

func TestContainsNegation(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{"no negation", "The sky is blue.", false},
		{"not prefix", "The sky is not blue.", true},
		{"never prefix", "It never works.", true},
		{"no prefix", "There is no way.", true},
		{"cannot prefix", "I cannot do this.", true},
		{"can't prefix", "I can't believe it.", true},
		{"don't prefix", "I don't know.", true},
		{"doesn't prefix", "It doesn't matter.", true},
		{"not at start", "Not today.", true},
		{"multiple negatives", "This is not correct and never was.", true},
		{"nope with not", "Nope, not happening.", true},
		{"nothing no match", "Nothing to see here.", false},
		{"no as substring", "Anode is a term.", false},
		{"negation without space", "No! Don't go!", true},
		// "No!" doesn't have trailing space, so "no " won't match
		// "Don't" -> matches "don't "
		{"uppercase negation", "It is NOT blue.", true},
		{"mixed case", "It is Not blue.", true},
		{"never at end", "This will never.", true},
		{"no inside word", "announce the decision", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsNegation(tt.content)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestContainsNegation_EmptyContent(t *testing.T) {
	assert.False(t, containsNegation(""), "empty content should not contain negation")
}

func TestContainsNegation_ContentShorterThanNegation(t *testing.T) {
	assert.True(t, containsNegation("no"), "'no' is a standalone negation word")
}

// ---------------------------------------------------------------------------
// Critical issues logged at warning level (via runChecks)
// ---------------------------------------------------------------------------

func TestHealthChecker_runChecks_CriticalIssuesHandled(t *testing.T) {
	// runChecks counts issues by severity and logs warnings for critical ones.
	// None of the current checks produce "critical" severity, but the code
	// handles the count and logging gracefully. This test verifies no panic.
	orphan := freshMemory("orphan-1", "tenant-1", "Orphan memory", nil, 3, 0)
	stale := makeHealthCheckerMemory("stale-1", "tenant-1", "Stale fact",
		[]string{"fact"}, 0, 0.5, types.VerdictNone,
		time.Now().Add(-200*24*time.Hour), time.Now().Add(-200*24*time.Hour),
	)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{hcSearchResult(orphan), hcSearchResult(stale)}, 2, nil
		},
	}
	hc := newTestHealthChecker(repo)

	hc.runChecks(context.Background())
	assert.Equal(t, 1, repo.searchCalls, "search should be called exactly once")
}

// ---------------------------------------------------------------------------
// Full cycle: AssessHealth with mixed issues from multiple checks
//
// Design: 6 memories producing exactly 5 issues (no graph fragmentation):
//   - orphan-1: no tags, hub=0 → orphan (medium)
//   - stale-1: tags, importance=0, 200d old → stale (low)
//   - wip-1: tags, WIP, 45d stale → verdict_consistency (warning)
//   - dup-a, dup-b: same content prefix → duplication (medium)
//   - normal: tags, recent, no issues → filler to dilute fragmentation
//
// Fragmentation check: stale-1 has |0|<2 and hub=0 → isolated.
// wip-1 has |3|>=2 → not isolated. Others have imp=3 → not isolated.
// 1/6 ≈ 16.7% < 30% → no fragmentation issue.
//
// Contradiction check: no negation words → no contradictions.
//
// Expected: orphan + stale + verdict + duplication = 4 issues
// Severity: low(1), medium(2), warning(1)
// ---------------------------------------------------------------------------

func TestHealthChecker_AssessHealth_AllChecksInOneCall(t *testing.T) {
	orphan := freshMemory("orphan-1", "tenant-1", "Orphan memory zero tags", nil, 3, 0)
	stale := makeHealthCheckerMemory("stale-1", "tenant-1", "Old fact from long ago",
		[]string{"fact"}, 0, 0.5, types.VerdictNone,
		time.Now().Add(-200*24*time.Hour), time.Now().Add(-200*24*time.Hour),
	)
	wipOld := makeHealthCheckerMemory("wip-1", "tenant-1", "WIP unfinished task",
		[]string{"tag1"}, 3, 0, types.VerdictWIP,
		time.Now().Add(-60*24*time.Hour), time.Now().Add(-45*24*time.Hour),
	)
	dupA := freshMemory("dup-a", "tenant-1", "Duplicate content prefix test memory A",
		[]string{"dup"}, 3, 0)
	dupB := freshMemory("dup-b", "tenant-1", "Duplicate content prefix test memory B",
		[]string{"dup"}, 3, 0)
	normal := freshMemory("normal-1", "tenant-1", "Normal memory with zero issues",
		[]string{"general"}, 3, 0)

	repo := &mockHealthCheckerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return []*types.MemorySearchResult{
				hcSearchResult(orphan), hcSearchResult(stale),
				hcSearchResult(wipOld), hcSearchResult(dupA),
				hcSearchResult(dupB), hcSearchResult(normal),
			}, 6, nil
		},
	}
	hc := newTestHealthChecker(repo)

	report, err := hc.AssessHealth(context.Background(), "tenant-1", "")
	require.NoError(t, err)

	// Expected: orphan (medium) + stale (low) + verdict (warning) + duplication (medium) = 4
	assert.Equal(t, 4, report.TotalIssues)
	assert.Equal(t, 1, report.BySeverity["low"], "stale")
	assert.Equal(t, 2, report.BySeverity["medium"], "orphan + duplication")
	assert.Equal(t, 1, report.BySeverity["warning"], "verdict_consistency")

	seenTypes := make(map[string]int)
	for _, issue := range report.Issues {
		seenTypes[issue.Type]++
	}
	assert.Equal(t, 1, seenTypes["orphan"])
	assert.Equal(t, 1, seenTypes["stale"])
	assert.Equal(t, 1, seenTypes["verdict_consistency"])
	assert.Equal(t, 1, seenTypes["duplication"])
}
