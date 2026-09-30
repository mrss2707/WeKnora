package memory_v2

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scopedMemoryServiceFake struct {
	interfaces.MemoryServiceV2
	readiness    types.MemoryV2Readiness
	search       []*types.MemorySearchResult
	searchFilter *types.MemoryFilter
	saved        *types.AgentMemory
	saveResult   *types.SaveMemoryResult
}

func (f *scopedMemoryServiceFake) Readiness() types.MemoryV2Readiness { return f.readiness }
func (f *scopedMemoryServiceFake) SearchMemories(_ context.Context, _ string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	copy := *filter
	f.searchFilter = &copy
	return f.search, nil
}
func (f *scopedMemoryServiceFake) SaveMemory(_ context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error) {
	f.saved = memory
	if f.saveResult != nil {
		return f.saveResult, nil
	}
	return &types.SaveMemoryResult{Memory: memory, Created: true}, nil
}

type scopedMemoryRepoFake struct {
	interfaces.MemoryRepositoryV2
	memories    map[string]*types.AgentMemory
	relations   []*types.MemoryRelation
	searchTotal int64
	searchErr   error
	lastFilter  *types.MemoryFilter
	getCalls    int
	getErr      map[string]error
}

func (f *scopedMemoryRepoFake) GetByID(_ context.Context, tenantID, id string) (*types.AgentMemory, error) {
	f.getCalls++
	if err := f.getErr[id]; err != nil {
		return nil, err
	}
	memory := f.memories[id]
	if memory == nil || memory.TenantID != tenantID {
		return nil, errors.New("not found")
	}
	return memory, nil
}
func (f *scopedMemoryRepoFake) GetRelations(_ context.Context, _, _ string) ([]*types.MemoryRelation, error) {
	return f.relations, nil
}
func (f *scopedMemoryRepoFake) Search(_ context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	copy := *filter
	f.lastFilter = &copy
	return nil, f.searchTotal, f.searchErr
}

func readyScopedService(repo *scopedMemoryRepoFake) (*ScopedMemoryV2Service, *scopedMemoryServiceFake) {
	base := &scopedMemoryServiceFake{readiness: types.MemoryV2Readiness{Ready: true, Reason: types.MemoryV2ReasonEnabled}}
	return &ScopedMemoryV2Service{service: base, repo: repo}, base
}

func TestScopedMemoryRecallUsesTrustedScopeAndFiltersResults(t *testing.T) {
	repo := &scopedMemoryRepoFake{}
	svc, base := readyScopedService(repo)
	base.search = []*types.MemorySearchResult{
		{Memory: &types.AgentMemory{ID: "ok", TenantID: "1", KbID: "kb-1"}},
		{Memory: &types.AgentMemory{ID: "other", TenantID: "1", KbID: "kb-2"}},
	}

	results, err := svc.Recall(context.Background(), interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}, "query", 500)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "ok", results[0].Memory.ID)
	assert.Equal(t, "1", base.searchFilter.TenantID)
	assert.Equal(t, "kb-1", base.searchFilter.KbID)
	assert.Equal(t, 50, base.searchFilter.Limit)
}

func TestScopedMemorySaveOverridesOwnershipAndPostChecksResult(t *testing.T) {
	repo := &scopedMemoryRepoFake{}
	svc, base := readyScopedService(repo)
	scope := interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}

	result, err := svc.Save(context.Background(), scope, "valid memory content", " session-1 ")

	require.NoError(t, err)
	assert.True(t, result.Created)
	assert.Equal(t, "1", base.saved.TenantID)
	assert.Equal(t, "kb-1", base.saved.KbID)
	assert.Empty(t, base.saved.UserID)
	assert.Equal(t, "session-1", base.saved.SessionID)

	base.saveResult = &types.SaveMemoryResult{Memory: &types.AgentMemory{TenantID: "1", KbID: "kb-2"}}
	_, err = svc.Save(context.Background(), scope, "valid memory content", "")
	assert.EqualError(t, err, errScopedMemoryNotFound.Error())
}

func TestScopedMemoryDetailAndGraphStayWithinKnowledgeBase(t *testing.T) {
	repo := &scopedMemoryRepoFake{memories: map[string]*types.AgentMemory{
		"focal": {ID: "focal", TenantID: "1", KbID: "kb-1"},
		"valid": {ID: "valid", TenantID: "1", KbID: "kb-1"},
		"other": {ID: "other", TenantID: "1", KbID: "kb-2"},
	}, relations: []*types.MemoryRelation{
		{ID: "r1", TenantID: "1", FromUUID: "focal", ToUUID: "valid", RelationType: "related_to", Weight: 0.8},
		{ID: "r2", TenantID: "1", FromUUID: "focal", ToUUID: "other", RelationType: "related_to", Weight: 0.9},
		{ID: "r3", TenantID: "1", FromUUID: "valid", ToUUID: "focal", RelationType: "justifies", Weight: 0.7},
	}}
	svc, _ := readyScopedService(repo)
	scope := interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}

	_, err := svc.Detail(context.Background(), scope, "other")
	assert.EqualError(t, err, errScopedMemoryNotFound.Error())

	graph, err := svc.Graph(context.Background(), scope, "focal", 100)
	require.NoError(t, err)
	require.Len(t, graph.Nodes, 1)
	assert.Equal(t, "valid", graph.Nodes[0].ID)
	require.Len(t, graph.Relations, 2)
	assert.Equal(t, "r1", graph.Relations[0].ID)
	assert.Equal(t, "r3", graph.Relations[1].ID)
}

func TestScopedMemoryGraphStopsLoadingAfterLimit(t *testing.T) {
	repo := &scopedMemoryRepoFake{memories: map[string]*types.AgentMemory{
		"focal": {ID: "focal", TenantID: "1", KbID: "kb-1"},
		"one":   {ID: "one", TenantID: "1", KbID: "kb-1"},
		"two":   {ID: "two", TenantID: "1", KbID: "kb-1"},
		"three": {ID: "three", TenantID: "1", KbID: "kb-1"},
	}, relations: []*types.MemoryRelation{
		{ID: "r1", FromUUID: "focal", ToUUID: "one", Weight: 1.0},
		{ID: "r2", FromUUID: "focal", ToUUID: "two", Weight: 0.9},
		{ID: "r3", FromUUID: "focal", ToUUID: "three", Weight: 0.8},
	}}
	svc, _ := readyScopedService(repo)

	graph, err := svc.Graph(context.Background(), interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}, "focal", 1)

	require.NoError(t, err)
	assert.Len(t, graph.Nodes, 1)
	assert.True(t, graph.Truncated)
	// Focal + first retained neighbor + one look-ahead proving truncation.
	assert.Equal(t, 3, repo.getCalls)
}

func TestScopedMemoryGraphReportsRelatedLookupFailure(t *testing.T) {
	repo := &scopedMemoryRepoFake{
		memories: map[string]*types.AgentMemory{
			"focal": {ID: "focal", TenantID: "1", KbID: "kb-1"},
		},
		relations: []*types.MemoryRelation{{ID: "r1", FromUUID: "focal", ToUUID: "broken"}},
		getErr:    map[string]error{"broken": errors.New("connection reset")},
	}
	svc, _ := readyScopedService(repo)

	_, err := svc.Graph(context.Background(), interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}, "focal", 10)

	assert.EqualError(t, err, "failed to load related memory")
}

func TestScopedMemoryStatusDoesNotLeakRepositoryErrors(t *testing.T) {
	repo := &scopedMemoryRepoFake{searchTotal: 7}
	svc, _ := readyScopedService(repo)

	status := svc.Status(context.Background(), "1")
	assert.True(t, status.Available)
	assert.Equal(t, int64(7), status.MemoryCount)
	assert.Equal(t, "1", repo.lastFilter.TenantID)

	repo.searchErr = errors.New("database details")
	status = svc.Status(context.Background(), "1")
	assert.False(t, status.Available)
	assert.Equal(t, "not_ready: repository query failed", status.Reason)
	assert.NotContains(t, status.Reason, "database details")
}

func TestScopedMemoryRejectsEmptyScope(t *testing.T) {
	svc, _ := readyScopedService(&scopedMemoryRepoFake{})
	_, err := svc.Recall(context.Background(), interfaces.MemoryV2Scope{}, "query", 10)
	assert.EqualError(t, err, "memory tenant scope is required")
}
