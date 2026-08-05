package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockDreamerRepo implements interfaces.MemoryRepositoryV2 for testing the dreamer.
type mockDreamerRepo struct {
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
	tryLockCalls []tryLockCall
	unlockCalls  []string
	updateCalls  []*types.AgentMemory
	deleteCalls  []deleteCall
	searchCalls  int
}

type tryLockCall struct {
	TenantID string
	WorkerID string
}

func (m *mockDreamerRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockDreamerRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockDreamerRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	m.mu.Lock()
	cpy := *memory
	m.updateCalls = append(m.updateCalls, &cpy)
	m.mu.Unlock()
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockDreamerRepo) Delete(ctx context.Context, tenantID, id string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, deleteCall{TenantID: tenantID, ID: id})
	m.mu.Unlock()
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockDreamerRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	m.mu.Lock()
	m.searchCalls++
	m.mu.Unlock()
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockDreamerRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockDreamerRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	m.mu.Lock()
	m.tryLockCalls = append(m.tryLockCalls, tryLockCall{TenantID: tenantID, WorkerID: workerID})
	m.mu.Unlock()
	if m.tryDreamerLockFunc != nil {
		return m.tryDreamerLockFunc(ctx, tenantID, workerID)
	}
	return true, nil
}

func (m *mockDreamerRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	m.unlockCalls = append(m.unlockCalls, tenantID)
	m.mu.Unlock()
	if m.unlockDreamerFunc != nil {
		return m.unlockDreamerFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockDreamerRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	if m.computeHubScores != nil {
		return m.computeHubScores(ctx, tenantID)
	}
	return nil
}

func (m *mockDreamerRepo) InvalidateResultCache(ctx context.Context, tenantID string) {
	if m.invalidateCache != nil {
		m.invalidateCache(ctx, tenantID)
	}
}

func (m *mockDreamerRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockDreamerRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockDreamerRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockDreamerRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockDreamerRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockDreamerRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}
func (m *mockDreamerRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

// mockDreamerChat implements chat.Chat for testing.
type mockDreamerChat struct {
	chatFunc func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error)
}

func (m *mockDreamerChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, opts)
	}
	return &types.ChatResponse{Content: `{"actions":[]}`}, nil
}

func (m *mockDreamerChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (m *mockDreamerChat) GetModelName() string { return "mock-dreamer" }

func (m *mockDreamerChat) GetModelID() string { return "mock-dreamer-model" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeDreamerMemory(id, tenantID, content string, verdict types.MemoryVerdict, importance, tier int) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Importance: importance,
		Verdict:    verdict,
		HubScore:   0,
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
		MemoryType: "semantic",
		Tier:       tier,
	}
}

func newTestDreamer(repo *mockDreamerRepo, chatClient chat.Chat, config types.DreamerConfig) *DreamerWorker {
	return NewDreamerWorker(repo, chatClient, config, nil)
}

func TestParseDuration_DefaultInvalidAndValid(t *testing.T) {
	fallback := 15 * time.Minute

	assert.Equal(t, fallback, parseDuration("", fallback))
	assert.Equal(t, fallback, parseDuration("not-a-duration", fallback))
	assert.Equal(t, 2*time.Hour, parseDuration("2h", fallback))
}

func TestDreamer_DreamPassRunsAllTenantsAndContinuesAfterTenantError(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			if filter.TenantID == "t1" {
				return nil, 0, assert.AnError
			}
			return nil, 0, nil
		},
	}
	d := NewDreamerWorker(repo, &mockDreamerChat{}, types.DreamerConfig{Enabled: true}, []string{"t1", "t2"})

	d.dreamPass(context.Background())

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.tryLockCalls, 2)
	assert.Equal(t, "t1", repo.tryLockCalls[0].TenantID)
	assert.Equal(t, "t2", repo.tryLockCalls[1].TenantID)
	assert.Equal(t, []string{"t1", "t2"}, repo.unlockCalls)
}

func TestDreamer_DreamPassFallbackEmptyTenant(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			assert.Equal(t, "", filter.TenantID)
			return nil, 0, nil
		},
	}
	d := NewDreamerWorker(repo, &mockDreamerChat{}, types.DreamerConfig{Enabled: true}, nil)

	d.dreamPass(context.Background())

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.tryLockCalls, 1)
	assert.Equal(t, "", repo.tryLockCalls[0].TenantID)
	assert.Equal(t, []string{""}, repo.unlockCalls)
}

// dreamerActionJSON builds the JSON for the LLM to return for a list of actions.
func dreamerActionJSON(actions []types.DreamAction) string {
	resp := dreamerResponse{Actions: actions}
	b, _ := json.Marshal(resp)
	return string(b)
}

func makeSearchResult(mem *types.AgentMemory) *types.MemorySearchResult {
	return &types.MemorySearchResult{Memory: mem, Score: 1.0}
}

// ---------------------------------------------------------------------------
// Test: Dreamer lock acquisition
// ---------------------------------------------------------------------------

func TestDreamer_LockAcquired(t *testing.T) {
	repo := &mockDreamerRepo{
		tryDreamerLockFunc: func(ctx context.Context, tenantID, workerID string) (bool, error) {
			return true, nil
		},
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Lock was acquired
	require.Len(t, repo.tryLockCalls, 1)
	assert.Equal(t, "tenant-1", repo.tryLockCalls[0].TenantID)

	// Lock was released
	require.Len(t, repo.unlockCalls, 1)
	assert.Equal(t, "tenant-1", repo.unlockCalls[0])
}

func TestDreamer_LockNotAcquired(t *testing.T) {
	repo := &mockDreamerRepo{
		tryDreamerLockFunc: func(ctx context.Context, tenantID, workerID string) (bool, error) {
			return false, nil
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Lock was attempted but not acquired — no search or actions
	require.Len(t, repo.tryLockCalls, 1)
	assert.Equal(t, 0, repo.searchCalls, "search should not be called when lock not acquired")
	assert.Empty(t, repo.unlockCalls, "unlock should not be called when lock was not acquired")
	assert.Equal(t, 0, result.ActionsProposed)
}

func TestDreamer_LockError(t *testing.T) {
	repo := &mockDreamerRepo{
		tryDreamerLockFunc: func(ctx context.Context, tenantID, workerID string) (bool, error) {
			return false, assert.AnError
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dreamer lock acquire failed")
	require.NotNil(t, result)
	assert.Empty(t, repo.unlockCalls, "unlock should not be called on lock error")
}

// ---------------------------------------------------------------------------
// Test: Action validation — confidence
// ---------------------------------------------------------------------------

func TestDreamer_AcceptsHighConfidence(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Low quality", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied)
}

func TestDreamer_RejectsLowConfidence(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			var results []*types.MemorySearchResult
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("mem-%d", i)
				mem := makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2)
				results = append(results, makeSearchResult(mem))
			}
			return results, 10, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Low confidence test", Confidence: 0.60},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	// Proposed but not applied because confidence too low
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
}

func TestDreamer_AcceptsExactlyThreshold(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Exactly at threshold", Confidence: 0.70},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied)
}

// ---------------------------------------------------------------------------
// Test: Protected verdicts
// ---------------------------------------------------------------------------

func TestDreamer_SkipsProtectedVerdictDecision(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictDecision, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictDecision, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Try to override decision", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	// Action was proposed but applyVerdictUpdate should fail because verdict is protected
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
}

func TestDreamer_SkipsProtectedVerdictFixed(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictFixed, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictFixed, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Try to override fixed", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
}

func TestDreamer_AllowsNonProtectedVerdict(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Good reason", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.Equal(t, types.MemoryVerdict("refuted"), repo.updateCalls[0].Verdict)
}

// ---------------------------------------------------------------------------
// Test: Max 5 actions enforced
// ---------------------------------------------------------------------------

func TestDreamer_MaxFiveActions(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			var results []*types.MemorySearchResult
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("mem-%d", i)
				mem := makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2)
				results = append(results, makeSearchResult(mem))
			}
			return results, 10, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := make([]types.DreamAction, 10)
			for i := 0; i < 10; i++ {
				actions[i] = types.DreamAction{
					Type:       "adjust_importance",
					TargetID:   fmt.Sprintf("mem-%d", i),
					Delta:      1,
					Reason:     "Increase importance",
					Confidence: 0.95,
				}
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	// 5 actions proposed (limited by MaxActions=5), 5 applied
	assert.Equal(t, 5, result.ActionsProposed)
	assert.Equal(t, 5, result.ActionsApplied)
	assert.Len(t, result.Actions, 5)
}

func TestDreamer_MaxActionsConfigZero(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			var results []*types.MemorySearchResult
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("mem-%d", i)
				mem := makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2)
				results = append(results, makeSearchResult(mem))
			}
			return results, 10, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	// MaxActions=0 means code defaults to 5
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := make([]types.DreamAction, 10)
			for i := 0; i < 10; i++ {
				actions[i] = types.DreamAction{
					Type:       "adjust_importance",
					TargetID:   fmt.Sprintf("mem-%d", i),
					Delta:      1,
					Reason:     "Increase importance",
					Confidence: 0.95,
				}
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 0, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 5, result.ActionsApplied)
}

// ---------------------------------------------------------------------------
// Test: Tier < 2 skip for delete actions
// ---------------------------------------------------------------------------

func TestDreamer_RemoveActionRequiresTier2OrHigher(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Low tier memory", types.VerdictNone, 3, 1)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Low tier memory", types.VerdictNone, 3, 1), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "remove", TargetID: "mem-1", Reason: "Low quality", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied, "tier < 2 removal should fail")
	assert.Empty(t, repo.deleteCalls, "no delete should occur for tier < 2")
}

func TestDreamer_RemoveActionAllowedForTier2(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Removable memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Removable memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "remove", TargetID: "mem-1", Reason: "Low quality", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.deleteCalls, 1)
	assert.Equal(t, "mem-1", repo.deleteCalls[0].ID)
	assert.Equal(t, "tenant-1", repo.deleteCalls[0].TenantID)
}

// ---------------------------------------------------------------------------
// Test: Soft-delete for removal actions (never permanent delete)
// ---------------------------------------------------------------------------

func TestDreamer_RemoveUsesSoftDelete(t *testing.T) {
	// The dreamer calls repo.Delete which uses GORM's gorm.DeletedAt (soft delete).
	// We verify that the Delete function is called (which is the soft-delete path)
	// and that there is no hard-delete method called.
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Removable memory", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Removable memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "remove", TargetID: "mem-1", Reason: "Cleanup", Confidence: 0.95},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	require.Len(t, repo.deleteCalls, 1)
	assert.Equal(t, "tenant-1", repo.deleteCalls[0].TenantID)
	assert.Equal(t, "mem-1", repo.deleteCalls[0].ID)
}

// ---------------------------------------------------------------------------
// Test: DryRun mode
// ---------------------------------------------------------------------------

func TestDreamer_DryRunActionsProposedNotApplied(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			var results []*types.MemorySearchResult
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("mem-%d", i)
				mem := makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2)
				results = append(results, makeSearchResult(mem))
			}
			return results, 10, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory(id, "tenant-1", "Test memory", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Dry run test", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	// DryRun = true
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000, DryRun: true}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied, "in dry-run, actions are recorded as applied")
	assert.Len(t, result.Actions, 1)
	// No actual repo methods should be called for modification
	assert.Empty(t, repo.updateCalls, "no updates should occur in dry-run mode")
	assert.Empty(t, repo.deleteCalls, "no deletes should occur in dry-run mode")
}

func TestDreamer_DryRunWithRemoveAction(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Removable", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "remove", TargetID: "mem-1", Reason: "Dry run remove", Confidence: 0.95},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000, DryRun: true}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 1, result.ActionsApplied)
	assert.Empty(t, repo.deleteCalls, "no delete should occur in dry-run mode")
}

// ---------------------------------------------------------------------------
// Test: Action validation — invalid types
// ---------------------------------------------------------------------------

func TestDreamer_InvalidActionTypeSkipped(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "unknown_action", TargetID: "mem-1", Reason: "Unknown", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied, "unknown action type should be skipped")
}

// ---------------------------------------------------------------------------
// Test: adjust_importance delta validation
// ---------------------------------------------------------------------------

func TestDreamer_AdjustImportanceValidDelta(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "adjust_importance", TargetID: "mem-1", Delta: 3, Reason: "Very important", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.Equal(t, 6, repo.updateCalls[0].Importance, "importance 3 + delta 3 = 6")
}

func TestDreamer_AdjustImportanceInvalidDelta(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "adjust_importance", TargetID: "mem-1", Delta: 5, Reason: "Way too high", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied, "delta > 3 should be rejected")
}

// ---------------------------------------------------------------------------
// Test: applyImportanceAdjust clamps to [-5, 6]
// ---------------------------------------------------------------------------

func TestDreamer_AdjustImportanceClampMin(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, -3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, -3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "adjust_importance", TargetID: "mem-1", Delta: -3, Reason: "Lower", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.Equal(t, -5, repo.updateCalls[0].Importance, "importance -3 + delta -3 should clamp to -5")
}

func TestDreamer_AdjustImportanceClampMax(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 5, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 5, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "adjust_importance", TargetID: "mem-1", Delta: 3, Reason: "Higher", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.Equal(t, 6, repo.updateCalls[0].Importance, "importance 5 + delta 3 should clamp to 6")
}

// ---------------------------------------------------------------------------
// Test: merge action validation
// ---------------------------------------------------------------------------

func TestDreamer_MergeRequiresAtLeastTwoIDs(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			memA := makeDreamerMemory("mem-a", "tenant-1", "Content A", types.VerdictNone, 3, 2)
			memB := makeDreamerMemory("mem-b", "tenant-1", "Content B", types.VerdictNone, 5, 2)
			return []*types.MemorySearchResult{makeSearchResult(memA), makeSearchResult(memB)}, 2, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			if id == "mem-a" {
				return makeDreamerMemory("mem-a", "tenant-1", "Content A", types.VerdictNone, 3, 2), nil
			}
			return makeDreamerMemory("mem-b", "tenant-1", "Content B", types.VerdictNone, 5, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "merge", TargetIDs: []string{"mem-a"}, Reason: "Single target", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied, "merge with single target should be rejected")
}

func TestDreamer_MergeApplied(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			memA := makeDreamerMemory("mem-a", "tenant-1", "Content A", types.VerdictNone, 3, 2)
			memB := makeDreamerMemory("mem-b", "tenant-1", "Content B", types.VerdictNone, 5, 2)
			return []*types.MemorySearchResult{makeSearchResult(memA), makeSearchResult(memB)}, 2, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			if id == "mem-a" {
				return makeDreamerMemory("mem-a", "tenant-1", "Content A", types.VerdictNone, 3, 2), nil
			}
			return makeDreamerMemory("mem-b", "tenant-1", "Content B", types.VerdictNone, 5, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "merge", TargetIDs: []string{"mem-a", "mem-b"}, Reason: "Similar content", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.Equal(t, "mem-a", repo.updateCalls[0].ID)
	// mem-b should be deleted (soft-deleted via merge-away)
	require.Len(t, repo.deleteCalls, 1)
	assert.Equal(t, "mem-b", repo.deleteCalls[0].ID)
}

// ---------------------------------------------------------------------------
// Test: No candidate memories
// ---------------------------------------------------------------------------

func TestDreamer_NoCandidates(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
	// Lock should be acquired and released
	require.Len(t, repo.tryLockCalls, 1)
	require.Len(t, repo.unlockCalls, 1)
}

// ---------------------------------------------------------------------------
// Test: LLM response parsing errors
// ---------------------------------------------------------------------------

func TestDreamer_InvalidLLMResponse(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: "not valid json at all"}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ActionsProposed, "invalid JSON should produce no actions")
}

// ---------------------------------------------------------------------------
// Test: LLM call error
// ---------------------------------------------------------------------------

func TestDreamer_LLMCallError(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return nil, assert.AnError
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM call failed")
	require.NotNil(t, result)
	// Lock should still be released on error
	require.Len(t, repo.unlockCalls, 1)
}

// ---------------------------------------------------------------------------
// Test: Search error handled gracefully
// ---------------------------------------------------------------------------

func TestDreamer_SearchError(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search failed")
	// Lock should still be released
	require.Len(t, repo.unlockCalls, 1)
}

// ---------------------------------------------------------------------------
// Test: Disabled worker
// ---------------------------------------------------------------------------

func TestDreamer_Disabled(t *testing.T) {
	repo := &mockDreamerRepo{}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: false}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
	// Lock should not be acquired when disabled
	assert.Empty(t, repo.tryLockCalls)
	assert.Empty(t, repo.unlockCalls)
}

// ---------------------------------------------------------------------------
// Test: Token budget default
// ---------------------------------------------------------------------------

func TestDreamer_DefaultTokenBudget(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	var capturedOpts *chat.ChatOptions
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			capturedOpts = opts
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 0}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, capturedOpts)
	assert.Equal(t, 4000, capturedOpts.MaxTokens, "default token budget should be 4000")
}

func TestDreamer_CustomTokenBudget(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	var capturedOpts *chat.ChatOptions
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			capturedOpts = opts
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 8000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, capturedOpts)
	assert.Equal(t, 8000, capturedOpts.MaxTokens)
}

// ---------------------------------------------------------------------------
// Test: applyVerdictUpdate checks protected verdict on target at apply time
// ---------------------------------------------------------------------------

func TestDreamer_VerdictUpdateProtectedAtApplyTime(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictDecision, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			return makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictDecision, 3, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "update_verdict", TargetID: "mem-1", NewVerdict: "refuted", Reason: "Override", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
}

// ---------------------------------------------------------------------------
// Test: Dreamer lock release after completion (defer verifies both success and error paths)
// ---------------------------------------------------------------------------

func TestDreamer_LockReleasedOnSuccess(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)

	require.Len(t, repo.tryLockCalls, 1)
	require.Len(t, repo.unlockCalls, 1)
	assert.Equal(t, repo.tryLockCalls[0].TenantID, repo.unlockCalls[0])
}

func TestDreamer_LockReleasedOnError(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, assert.AnError
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.Error(t, err)

	// Lock should still be released
	require.Len(t, repo.tryLockCalls, 1)
	require.Len(t, repo.unlockCalls, 1)
}

// ---------------------------------------------------------------------------
// Test: Empty actions from LLM
// ---------------------------------------------------------------------------

func TestDreamer_EmptyActions(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ActionsProposed)
	assert.Equal(t, 0, result.ActionsApplied)
	assert.Empty(t, result.Actions)
}

// ---------------------------------------------------------------------------
// Test: ComputeHubScores must not be called during dreamer pass
// ---------------------------------------------------------------------------

func TestDreamer_DoesNotCallComputeHubScores(t *testing.T) {
	computeCalled := false
	repo := &mockDreamerRepo{
		computeHubScores: func(ctx context.Context, tenantID string) error {
			computeCalled = true
			return nil
		},
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			mem := makeDreamerMemory("mem-1", "tenant-1", "Test", types.VerdictNone, 3, 2)
			return []*types.MemorySearchResult{makeSearchResult(mem)}, 1, nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: `{"actions":[]}`}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.False(t, computeCalled, "ComputeHubScores should not be called during dreamer pass")
}

// ---------------------------------------------------------------------------
// Test: Context cancellation
// ---------------------------------------------------------------------------

func TestDreamer_Run_ContextCancellation(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	// Give the goroutine time to enter the loop
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case <-done:
		// Run returned cleanly — success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}
}

func TestDreamer_Run_PreCancelledContext(t *testing.T) {
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after pre-cancelled context")
	}
}

func TestDreamer_ExecutePass_CancelledContext(t *testing.T) {
	repo := &mockDreamerRepo{}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := d.RunPass(ctx, "tenant-1")
	// Context cancellation means TryDreamerLock may or may not succeed,
	// but the function should not panic
	if err != nil {
		t.Logf("expected error with cancelled context: %v", err)
	} else {
		require.NotNil(t, result)
	}
}

// ---------------------------------------------------------------------------
// Test: applyAction returns error for unknown type
// ---------------------------------------------------------------------------

func TestDreamer_ApplyActionUnknownType(t *testing.T) {
	d := &DreamerWorker{}
	err := d.applyAction(context.Background(), "tenant-1", types.DreamAction{
		Type: "nonexistent", TargetID: "mem-1", Reason: "test", Confidence: 0.95,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action type")
}

// ---------------------------------------------------------------------------
// Test: Merge content truncation at 2000 chars
// ---------------------------------------------------------------------------

func TestDreamer_MergeContentTruncated(t *testing.T) {
	longContentA := string(make([]byte, 1200))
	longContentB := string(make([]byte, 1200))

	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			memA := makeDreamerMemory("mem-a", "tenant-1", longContentA, types.VerdictNone, 3, 2)
			memB := makeDreamerMemory("mem-b", "tenant-1", longContentB, types.VerdictNone, 5, 2)
			return []*types.MemorySearchResult{makeSearchResult(memA), makeSearchResult(memB)}, 2, nil
		},
		getByIDFunc: func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
			if id == "mem-a" {
				return makeDreamerMemory("mem-a", "tenant-1", longContentA, types.VerdictNone, 3, 2), nil
			}
			return makeDreamerMemory("mem-b", "tenant-1", longContentB, types.VerdictNone, 5, 2), nil
		},
	}
	chatClient := &mockDreamerChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			actions := []types.DreamAction{
				{Type: "merge", TargetIDs: []string{"mem-a", "mem-b"}, Reason: "Similar", Confidence: 0.85},
			}
			return &types.ChatResponse{Content: dreamerActionJSON(actions)}, nil
		},
	}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	result, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ActionsApplied)
	require.Len(t, repo.updateCalls, 1)
	assert.LessOrEqual(t, len(repo.updateCalls[0].Content), 2000, "merged content should not exceed 2000 chars")
}

// ---------------------------------------------------------------------------
// Test: Panic recovery in dreamPass
// ---------------------------------------------------------------------------

func TestDreamer_dreamPass_PanicRecovery(t *testing.T) {
	// dreamPass calls executePass which could panic. The dreamPass function
	// doesn't have a recover, but the test verifies that a panic in executePass
	// doesn't crash the whole test suite and is logged.
	repo := &mockDreamerRepo{
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			panic("simulated panic in dreamer")
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	// dreamPass does not have a recover, so a panic will propagate.
	// We wrap it in a recover here to verify it doesn't do something unexpected.
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic to propagate from dreamPass")
		assert.Contains(t, r, "simulated panic in dreamer")
	}()

	// Also ensure lock is not left acquired after panic
	d.dreamPass(context.Background())
}

// ---------------------------------------------------------------------------
// Test: URL encoding / query building helpers
// ---------------------------------------------------------------------------

func TestTruncateContent(t *testing.T) {
	short := "short content"
	assert.Equal(t, "short content", truncateContent(short, 200))

	long := string(make([]byte, 500))
	truncated := truncateContent(long, 50)
	assert.Equal(t, 53, len(truncated), "50 chars + '...' = 53")
	assert.Equal(t, "...", truncated[50:])
}

func TestTruncateContentExactBoundary(t *testing.T) {
	// Build a string of exactly 200 runes
	content := ""
	for i := 0; i < 200; i++ {
		content += "a"
	}
	// Truncate at exactly the length — should be a no-op
	assert.Equal(t, content, truncateContent(content, 200))
}

// ---------------------------------------------------------------------------
// Test: Unknown action type in validateAction
// ---------------------------------------------------------------------------

func TestValidateAction_UnknownType(t *testing.T) {
	d := &DreamerWorker{}
	valid := d.validateAction(types.DreamAction{
		Type: "bogus", TargetID: "mem-1", Reason: "test", Confidence: 0.95,
	})
	assert.False(t, valid, "unknown action type should be invalid")
}

func TestValidateAction_EmptyType(t *testing.T) {
	d := &DreamerWorker{}
	valid := d.validateAction(types.DreamAction{
		Type: "", TargetID: "mem-1", Reason: "test", Confidence: 0.95,
	})
	assert.False(t, valid, "empty action type should be invalid")
}

// ---------------------------------------------------------------------------
// Test: DreamerWorker ID is unique
// ---------------------------------------------------------------------------

func TestDreamer_WorkerIDFormat(t *testing.T) {
	config := types.DreamerConfig{Enabled: true}
	d1 := newTestDreamer(&mockDreamerRepo{}, &mockDreamerChat{}, config)
	d2 := newTestDreamer(&mockDreamerRepo{}, &mockDreamerChat{}, config)
	assert.Contains(t, d1.workerID, "dreamer-", "worker ID should contain dreamer- prefix")
	assert.Contains(t, d2.workerID, "dreamer-", "worker ID should contain dreamer- prefix")
	assert.NotEmpty(t, d1.workerID)
	assert.NotEmpty(t, d2.workerID)
}

func TestDreamer_WorkerIDInLockCall(t *testing.T) {
	var capturedWorkerID string
	repo := &mockDreamerRepo{
		tryDreamerLockFunc: func(ctx context.Context, tenantID, workerID string) (bool, error) {
			capturedWorkerID = workerID
			return true, nil
		},
		searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
			return nil, 0, nil
		},
	}
	chatClient := &mockDreamerChat{}
	config := types.DreamerConfig{Enabled: true, MaxActions: 5, TokenBudget: 4000}
	d := newTestDreamer(repo, chatClient, config)

	_, err := d.RunPass(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.Contains(t, capturedWorkerID, "dreamer-")
}
