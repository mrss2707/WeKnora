package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type memoryV2HandlerServiceFake struct {
	saveResult *types.SaveMemoryResult
	saveErr    error
	saved      *types.AgentMemory
	saveCalls  int

	searchResults []*types.MemorySearchResult
	searchErr     error
	searchQuery   string
	searchFilter  *types.MemoryFilter
	searchCalls   int

	healthIssues []*types.MemoryHealthIssue
	healthErr    error
	healthTenant string
	healthKbID   string
	healthCalls  int

	dreamResult *types.DreamResult
	dreamErr    error
	dreamTenant string
	dreamCalls  int
}

var _ interfaces.MemoryServiceV2 = (*memoryV2HandlerServiceFake)(nil)

func (f *memoryV2HandlerServiceFake) AddEpisode(context.Context, string, string, string, []types.Message) error {
	return nil
}

func (f *memoryV2HandlerServiceFake) RetrieveMemory(context.Context, string, string) (*types.MemoryContext, error) {
	return nil, nil
}

func (f *memoryV2HandlerServiceFake) SaveMemory(_ context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error) {
	f.saveCalls++
	copy := *memory
	f.saved = &copy
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.saveResult != nil {
		return f.saveResult, nil
	}
	return &types.SaveMemoryResult{Memory: memory, Created: true}, nil
}

func (f *memoryV2HandlerServiceFake) SearchMemories(_ context.Context, query string, filter *types.MemoryFilter) ([]*types.MemorySearchResult, error) {
	f.searchCalls++
	f.searchQuery = query
	f.searchFilter = cloneMemoryFilter(filter)
	return f.searchResults, f.searchErr
}

func (f *memoryV2HandlerServiceFake) ConsolidateDream(_ context.Context, tenantID string) (*types.DreamResult, error) {
	f.dreamCalls++
	f.dreamTenant = tenantID
	return f.dreamResult, f.dreamErr
}

func (f *memoryV2HandlerServiceFake) AssessHealth(_ context.Context, tenantID, kbID string) ([]*types.MemoryHealthIssue, error) {
	f.healthCalls++
	f.healthTenant = tenantID
	f.healthKbID = kbID
	return f.healthIssues, f.healthErr
}

func (f *memoryV2HandlerServiceFake) StartWorkers(context.Context) {}
func (f *memoryV2HandlerServiceFake) Cleanup()                     {}

type memoryV2HandlerRepoFake struct {
	searchResults []*types.MemorySearchResult
	searchTotals  []int64
	searchTotal   int64
	searchErr     error
	searchCalls   int
	searchFilters []*types.MemoryFilter

	getMemory *types.AgentMemory
	getErr    error
	getCalls  int
	getTenant string
	getID     string

	deleteErr    error
	deleteCalls  int
	deleteTenant string
	deleteID     string
}

var _ interfaces.MemoryRepositoryV2 = (*memoryV2HandlerRepoFake)(nil)

func (f *memoryV2HandlerRepoFake) Create(context.Context, *types.AgentMemory) error { return nil }

func (f *memoryV2HandlerRepoFake) GetByID(_ context.Context, tenantID, id string) (*types.AgentMemory, error) {
	f.getCalls++
	f.getTenant = tenantID
	f.getID = id
	return f.getMemory, f.getErr
}

func (f *memoryV2HandlerRepoFake) GetByFingerprint(context.Context, string, string) (*types.AgentMemory, error) {
	return nil, nil
}

func (f *memoryV2HandlerRepoFake) Update(context.Context, *types.AgentMemory) error { return nil }

func (f *memoryV2HandlerRepoFake) Delete(_ context.Context, tenantID, id string) error {
	f.deleteCalls++
	f.deleteTenant = tenantID
	f.deleteID = id
	return f.deleteErr
}

func (f *memoryV2HandlerRepoFake) CreateRelation(context.Context, *types.MemoryRelation) error {
	return nil
}
func (f *memoryV2HandlerRepoFake) GetRelations(context.Context, string, string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (f *memoryV2HandlerRepoFake) DeleteRelation(context.Context, string, string) error { return nil }

func (f *memoryV2HandlerRepoFake) Search(_ context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	f.searchCalls++
	f.searchFilters = append(f.searchFilters, cloneMemoryFilter(filter))
	if f.searchErr != nil {
		return nil, 0, f.searchErr
	}
	if len(f.searchTotals) >= f.searchCalls {
		return f.searchResults, f.searchTotals[f.searchCalls-1], nil
	}
	return f.searchResults, f.searchTotal, nil
}

func (f *memoryV2HandlerRepoFake) CosineSearch(context.Context, *types.MemoryFilter, []float32, int) ([]*types.MemorySearchResult, error) {
	return nil, nil
}
func (f *memoryV2HandlerRepoFake) TryDreamerLock(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *memoryV2HandlerRepoFake) UnlockDreamer(context.Context, string) error { return nil }
func (f *memoryV2HandlerRepoFake) ComputeHubScores(context.Context, string) error {
	return nil
}
func (f *memoryV2HandlerRepoFake) HardDeleteExpired(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func (f *memoryV2HandlerRepoFake) InvalidateResultCache(context.Context, string)   {}
func (f *memoryV2HandlerRepoFake) SetCacheInvalidator(interfaces.CacheInvalidator) {}
func (f *memoryV2HandlerRepoFake) GetEmbeddingDimension(context.Context, string) (int, error) {
	return 0, nil
}

func cloneMemoryFilter(filter *types.MemoryFilter) *types.MemoryFilter {
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

func newMemoryV2HandlerTestContext(method, path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		requestBody = bytes.NewReader(payload)
	}

	req := httptest.NewRequest(method, path, requestBody)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, recorder
}

func newMemoryV2HandlerRawContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, recorder
}

func setMemoryV2Auth(c *gin.Context, tenantID uint64, userID string) {
	c.Set(types.TenantIDContextKey.String(), tenantID)
	if userID != "" {
		c.Set(types.UserIDContextKey.String(), userID)
	}
}

func decodeJSONBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func jsonMap(t *testing.T, value any) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	require.Truef(t, ok, "expected map[string]any, got %T", value)
	return m
}

func jsonSlice(t *testing.T, value any) []any {
	t.Helper()
	s, ok := value.([]any)
	require.Truef(t, ok, "expected []any, got %T", value)
	return s
}

func TestMemoryV2Handler_getTenantID(t *testing.T) {
	h := &MemoryV2Handler{}

	c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	got, ok := h.getTenantID(c)
	assert.False(t, ok)
	assert.Empty(t, got)

	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	setMemoryV2Auth(c, 0, "")
	got, ok = h.getTenantID(c)
	assert.False(t, ok)
	assert.Empty(t, got)

	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	setMemoryV2Auth(c, 42, "")
	got, ok = h.getTenantID(c)
	assert.True(t, ok)
	assert.Equal(t, "42", got)
}

func TestMemoryV2Handler_getUserID(t *testing.T) {
	h := &MemoryV2Handler{}

	c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	got, ok := h.getUserID(c)
	assert.False(t, ok)
	assert.Empty(t, got)

	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	c.Set(types.UserIDContextKey.String(), uint64(12))
	got, ok = h.getUserID(c)
	assert.False(t, ok)
	assert.Empty(t, got)

	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/", nil)
	setMemoryV2Auth(c, 42, "user-1")
	got, ok = h.getUserID(c)
	assert.True(t, ok)
	assert.Equal(t, "user-1", got)
}

func TestParseOptionalVerdicts(t *testing.T) {
	assert.Nil(t, parseOptionalVerdicts(""))
	assert.Nil(t, parseOptionalVerdicts(" \t "))
	assert.Equal(t,
		[]types.MemoryVerdict{types.VerdictFixed, types.VerdictDecision},
		parseOptionalVerdicts(" fixed, ,decision, "),
	)
}

func TestMemoryV2Handler_ListMemories(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchTotal: 9}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories?kb_id=kb1&page=2&page_size=5&memory_type=semantic&session_id=s1&tier=2&verdicts=fixed,decision", nil)
		setMemoryV2Auth(c, 42, "user-1")

		h.ListMemories(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, 1, repo.searchCalls)
		filter := repo.searchFilters[0]
		assert.Equal(t, "42", filter.TenantID)
		assert.Equal(t, "kb1", filter.KbID)
		assert.Equal(t, 5, filter.Limit)
		assert.Equal(t, 5, filter.Offset)
		assert.Equal(t, "semantic", filter.MemoryType)
		assert.Equal(t, "s1", filter.SessionID)
		require.NotNil(t, filter.Tier)
		assert.Equal(t, 2, *filter.Tier)
		assert.Equal(t, []types.MemoryVerdict{types.VerdictFixed, types.VerdictDecision}, filter.Verdicts)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, float64(9), data["total"])
		assert.Equal(t, float64(2), data["page"])
		assert.Equal(t, float64(5), data["page_size"])
	})

	t.Run("missing tenant", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories", nil)

		h.ListMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")
		assert.Zero(t, repo.searchCalls)
	})

	t.Run("invalid pagination", func(t *testing.T) {
		for _, path := range []string{"/memories?page=0", "/memories?page_size=999"} {
			repo := &memoryV2HandlerRepoFake{}
			h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
			c, _ := newMemoryV2HandlerTestContext(http.MethodGet, path, nil)
			setMemoryV2Auth(c, 42, "")

			h.ListMemories(c)

			require.Len(t, c.Errors, 1)
			assert.Zero(t, repo.searchCalls)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchErr: errors.New("db down")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories", nil)
		setMemoryV2Auth(c, 42, "")

		h.ListMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to list memories")
		assert.Empty(t, recorder.Body.String())
	})
}

func TestMemoryV2Handler_GetMemory(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		mem := &types.AgentMemory{ID: "mem-1", TenantID: "42", Content: "hello"}
		repo := &memoryV2HandlerRepoFake{getMemory: mem}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemory(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "42", repo.getTenant)
		assert.Equal(t, "mem-1", repo.getID)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, "mem-1", data["id"])
	})

	t.Run("not found", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{getErr: errors.New("missing")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemory(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory not found")
	})

	t.Run("missing tenant and id", func(t *testing.T) {
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/", nil)
		h.GetMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")

		c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/", nil)
		setMemoryV2Auth(c, 42, "")
		h.GetMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory ID")
	})
}

func TestMemoryV2Handler_CreateMemory(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{saveResult: &types.SaveMemoryResult{Created: true}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "hello", "kb_id": "kb1"})
		setMemoryV2Auth(c, 42, "user-1")

		h.CreateMemory(c)

		require.Equal(t, http.StatusCreated, recorder.Code)
		require.Equal(t, 1, svc.saveCalls)
		assert.Equal(t, "42", svc.saved.TenantID)
		assert.Equal(t, "user-1", svc.saved.UserID)
		assert.Equal(t, "kb1", svc.saved.KbID)
	})

	t.Run("duplicate", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{saveResult: &types.SaveMemoryResult{Created: false}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "hello"})
		setMemoryV2Auth(c, 42, "")

		h.CreateMemory(c)

		assert.Equal(t, http.StatusOK, recorder.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerRawContext(http.MethodPost, "/memories", "{")
		setMemoryV2Auth(c, 42, "")

		h.CreateMemory(c)

		require.Len(t, c.Errors, 1)
		assert.Zero(t, svc.saveCalls)
	})

	t.Run("service error and missing tenant", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{saveErr: errors.New("save failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "hello"})
		setMemoryV2Auth(c, 42, "")

		h.CreateMemory(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to save memory")

		c, _ = newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "hello"})
		h.CreateMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Equal(t, 1, svc.saveCalls)
	})
}

func TestMemoryV2Handler_UpdateMemory(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{saveResult: &types.SaveMemoryResult{Created: false}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"content": "updated"})
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "user-1")

		h.UpdateMemory(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "mem-1", svc.saved.ID)
		assert.Equal(t, "42", svc.saved.TenantID)
		assert.Equal(t, "user-1", svc.saved.UserID)
	})

	t.Run("error branches", func(t *testing.T) {
		tests := []struct {
			name    string
			ctx     func(*memoryV2HandlerServiceFake) (*gin.Context, *httptest.ResponseRecorder)
			wantErr string
		}{
			{
				name: "missing tenant",
				ctx: func(*memoryV2HandlerServiceFake) (*gin.Context, *httptest.ResponseRecorder) {
					c, r := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"content": "updated"})
					c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
					return c, r
				},
				wantErr: "Tenant ID",
			},
			{
				name: "missing id",
				ctx: func(*memoryV2HandlerServiceFake) (*gin.Context, *httptest.ResponseRecorder) {
					c, r := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/", map[string]any{"content": "updated"})
					setMemoryV2Auth(c, 42, "")
					return c, r
				},
				wantErr: "Memory ID",
			},
			{
				name: "invalid json",
				ctx: func(*memoryV2HandlerServiceFake) (*gin.Context, *httptest.ResponseRecorder) {
					c, r := newMemoryV2HandlerRawContext(http.MethodPut, "/memories/mem-1", "{")
					c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
					setMemoryV2Auth(c, 42, "")
					return c, r
				},
				wantErr: "Invalid request",
			},
			{
				name: "service error",
				ctx: func(svc *memoryV2HandlerServiceFake) (*gin.Context, *httptest.ResponseRecorder) {
					svc.saveErr = errors.New("save failed")
					c, r := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"content": "updated"})
					c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
					setMemoryV2Auth(c, 42, "")
					return c, r
				},
				wantErr: "Failed to update memory",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svc := &memoryV2HandlerServiceFake{}
				h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
				c, _ := tt.ctx(svc)
				h.UpdateMemory(c)
				require.Len(t, c.Errors, 1)
				assert.Contains(t, c.Errors.Last().Error(), tt.wantErr)
			})
		}
	})
}

func TestMemoryV2Handler_DeleteMemory(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.DeleteMemory(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "42", repo.deleteTenant)
		assert.Equal(t, "mem-1", repo.deleteID)
	})

	t.Run("repo error missing id missing tenant", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{deleteErr: errors.New("delete failed")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, _ := newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")
		h.DeleteMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to delete memory")

		c, _ = newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/", nil)
		setMemoryV2Auth(c, 42, "")
		h.DeleteMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory ID")

		c, _ = newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		h.DeleteMemory(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")
	})
}

func TestMemoryV2Handler_SearchMemories(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{searchResults: []*types.MemorySearchResult{{Memory: &types.AgentMemory{ID: "mem-1"}, Score: 0.9}}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=hello&kb_id=kb1&memory_type=semantic&session_id=s1&limit=7&min_score=0.42&verdicts=fixed,refuted", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "hello", svc.searchQuery)
		filter := svc.searchFilter
		assert.Equal(t, "42", filter.TenantID)
		assert.Equal(t, "kb1", filter.KbID)
		assert.Equal(t, "semantic", filter.MemoryType)
		assert.Equal(t, "s1", filter.SessionID)
		assert.Equal(t, 7, filter.Limit)
		assert.InDelta(t, 0.42, filter.MinScore, 0.0001)
		assert.Equal(t, []types.MemoryVerdict{types.VerdictFixed, types.VerdictRefuted}, filter.Verdicts)
	})

	t.Run("empty query validation", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=%20%20", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Zero(t, svc.searchCalls)
	})

	t.Run("malformed optional numeric query defaults", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=hello&limit=bad&min_score=bad", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		assert.Empty(t, c.Errors)
		assert.Zero(t, svc.searchFilter.Limit)
		assert.Zero(t, svc.searchFilter.MinScore)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{searchErr: errors.New("search failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=hello", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to search memories")
	})
}

func TestMemoryV2Handler_GetMemoryGraph(t *testing.T) {
	t.Run("happy path deduplicates nodes", func(t *testing.T) {
		focal := &types.AgentMemory{ID: "mem-1", TenantID: "42", Content: "focal memory", MemoryType: "semantic", Verdict: types.VerdictDecision, HubScore: 1.5}
		related := &types.AgentMemory{ID: "mem-2", TenantID: "42", Content: "related memory", MemoryType: "episodic", Verdict: types.VerdictFixed, HubScore: 0.5}
		repo := &memoryV2HandlerRepoFake{
			getMemory: focal,
			searchResults: []*types.MemorySearchResult{
				{Memory: nil, Score: 0.99},
				{Memory: focal, Score: 0.98},
				{Memory: related, Score: 0.97},
				{Memory: related, Score: 0.96},
			},
		}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/mem-1?kb_id=kb1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryGraph(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		filter := repo.searchFilters[0]
		assert.Equal(t, "42", filter.TenantID)
		assert.Equal(t, "kb1", filter.KbID)
		assert.Equal(t, 20, filter.Limit)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		nodes := jsonSlice(t, data["nodes"])
		edges := jsonSlice(t, data["edges"])
		assert.Len(t, nodes, 2)
		assert.Len(t, edges, 2)
	})

	t.Run("get error", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{getErr: errors.New("missing")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryGraph(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory not found")
	})

	t.Run("search error is non fatal", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{getMemory: &types.AgentMemory{ID: "mem-1", Content: "focal"}, searchErr: errors.New("search failed")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryGraph(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Len(t, jsonSlice(t, data["nodes"]), 1)
		assert.Empty(t, jsonSlice(t, data["edges"]))
	})
}

func TestMemoryV2Handler_GetMemoryStats(t *testing.T) {
	t.Run("happy path documents per type filters", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchTotals: []int64{10, 4, 3, 0}}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/stats?kb_id=kb1", nil)
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryStats(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Len(t, repo.searchFilters, 4)
		assert.Equal(t, "kb1", repo.searchFilters[0].KbID)
		assert.Empty(t, repo.searchFilters[1].KbID, "current per-type count filters do not carry kb_id")
		assert.Equal(t, "semantic", repo.searchFilters[1].MemoryType)
		assert.Equal(t, "episodic", repo.searchFilters[2].MemoryType)
		assert.Equal(t, "procedural", repo.searchFilters[3].MemoryType)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, float64(10), data["total_memories"])
		byType := jsonMap(t, data["by_type"])
		assert.Equal(t, float64(4), byType["semantic"])
		assert.Equal(t, float64(3), byType["episodic"])
		assert.NotContains(t, byType, "procedural")
	})

	t.Run("first search error", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchErr: errors.New("db down")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/stats", nil)
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryStats(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to get memory stats")
	})
}

func TestMemoryV2Handler_GetHealthReport(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{healthIssues: []*types.MemoryHealthIssue{{Severity: "low"}, {Severity: "critical"}, {Severity: "critical"}}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health?kb_id=kb1", nil)
		setMemoryV2Auth(c, 42, "")

		h.GetHealthReport(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "42", svc.healthTenant)
		assert.Equal(t, "kb1", svc.healthKbID)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, "42", data["tenant_id"])
		assert.Equal(t, float64(3), data["total_issues"])
		bySeverity := jsonMap(t, data["by_severity"])
		assert.Equal(t, float64(1), bySeverity["low"])
		assert.Equal(t, float64(2), bySeverity["critical"])
	})

	t.Run("service error and missing tenant", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{healthErr: errors.New("health failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health", nil)
		setMemoryV2Auth(c, 42, "")
		h.GetHealthReport(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to assess health")

		c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health", nil)
		h.GetHealthReport(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")
	})
}

func TestMemoryV2Handler_TriggerDream(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{dreamResult: &types.DreamResult{ActionsProposed: 2, ActionsApplied: 1}}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", nil)
		setMemoryV2Auth(c, 42, "")

		h.TriggerDream(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "42", svc.dreamTenant)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, float64(2), data["actions_proposed"])
	})

	t.Run("service error and missing tenant", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{dreamErr: errors.New("dream failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{})
		c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", nil)
		setMemoryV2Auth(c, 42, "")
		h.TriggerDream(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to trigger dream")

		c, _ = newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", nil)
		h.TriggerDream(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")
	})
}

func TestMemoryV2Handler_MemoryStatus(t *testing.T) {
	t.Run("nil repo", func(t *testing.T) {
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, "v2", body["backend"])
		assert.Equal(t, false, body["available"])
	})

	t.Run("missing tenant", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, "v2", body["backend"])
		assert.Equal(t, true, body["available"])
		assert.Zero(t, repo.searchCalls)
	})

	t.Run("repo search success", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchTotal: 12}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)
		setMemoryV2Auth(c, 42, "")

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, true, body["available"])
		assert.Equal(t, float64(12), body["memory_count"])
		assert.Equal(t, "42", repo.searchFilters[0].TenantID)
	})

	t.Run("repo search error", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchErr: errors.New("db down")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)
		setMemoryV2Auth(c, 42, "")

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, true, body["available"])
		assert.NotContains(t, body, "memory_count")
	})
}
