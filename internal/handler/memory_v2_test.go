package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
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

	// readiness, when non-nil, is returned by Readiness; nil means the
	// default enabled state.
	readiness *types.MemoryV2Readiness

	patchResult *types.AgentMemory
	patchErr    error
	patchTenant string
	patchID     string
	patch       *interfaces.MemoryV2Patch
	patchCalls  int
}

var (
	_ interfaces.MemoryServiceV2 = (*memoryV2HandlerServiceFake)(nil)
	_ interfaces.MemoryV2Patcher = (*memoryV2HandlerServiceFake)(nil)
)

func (f *memoryV2HandlerServiceFake) PatchMemory(_ context.Context, tenantID, id string, patch interfaces.MemoryV2Patch) (*types.AgentMemory, error) {
	f.patchCalls++
	f.patchTenant, f.patchID, f.patch = tenantID, id, &patch
	return f.patchResult, f.patchErr
}

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

func (f *memoryV2HandlerServiceFake) Readiness() types.MemoryV2Readiness {
	if f.readiness == nil {
		return types.MemoryV2Readiness{Ready: true, Reason: types.MemoryV2ReasonEnabled}
	}
	return *f.readiness
}

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

// setMemoryV2Auth mirrors the auth middleware: identity lands both on the gin
// context and on the request context.
func setMemoryV2Auth(c *gin.Context, tenantID uint64, userID string) {
	c.Set(types.TenantIDContextKey.String(), tenantID)
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	if userID != "" {
		c.Set(types.UserIDContextKey.String(), userID)
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	c.Request = c.Request.WithContext(ctx)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories", nil)

		h.ListMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Tenant ID")
		assert.Zero(t, repo.searchCalls)
	})

	t.Run("invalid pagination", func(t *testing.T) {
		for _, path := range []string{"/memories?page=0", "/memories?page_size=999"} {
			repo := &memoryV2HandlerRepoFake{}
			h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
			c, _ := newMemoryV2HandlerTestContext(http.MethodGet, path, nil)
			setMemoryV2Auth(c, 42, "")

			h.ListMemories(c)

			require.Len(t, c.Errors, 1)
			assert.Zero(t, repo.searchCalls)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchErr: errors.New("db down")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")

		h.GetMemory(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory not found")
	})

	t.Run("missing tenant and id", func(t *testing.T) {
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "hello"})
		setMemoryV2Auth(c, 42, "")

		h.CreateMemory(c)

		assert.Equal(t, http.StatusOK, recorder.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, _ := newMemoryV2HandlerRawContext(http.MethodPost, "/memories", "{")
		setMemoryV2Auth(c, 42, "")

		h.CreateMemory(c)

		require.Len(t, c.Errors, 1)
		assert.Zero(t, svc.saveCalls)
	})

	t.Run("service error and missing tenant", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{saveErr: errors.New("save failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
	t.Run("metadata patch returns the updated memory", func(t *testing.T) {
		updated := &types.AgentMemory{ID: "mem-1", TenantID: "42", Verdict: types.VerdictGotcha, Importance: 0}
		svc := &memoryV2HandlerServiceFake{patchResult: updated}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"verdict": "gotcha", "importance": 0})
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "user-1")

		h.UpdateMemory(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, 1, svc.patchCalls)
		assert.Zero(t, svc.saveCalls, "a metadata patch never re-runs ingestion")
		assert.Equal(t, "42", svc.patchTenant)
		assert.Equal(t, "mem-1", svc.patchID)
		require.NotNil(t, svc.patch.Verdict)
		assert.Equal(t, types.VerdictGotcha, *svc.patch.Verdict)
		require.NotNil(t, svc.patch.Importance, "explicit importance 0 must not be dropped")
		assert.Equal(t, 0, *svc.patch.Importance)
		assert.Nil(t, svc.patch.Tier)
		data := jsonMap(t, decodeJSONBody(t, recorder)["data"])
		assert.Equal(t, "mem-1", data["id"], "PUT responds with the memory itself")
		assert.NotContains(t, data, "embedding")
	})

	t.Run("error branches", func(t *testing.T) {
		tests := []struct {
			name     string
			body     any
			raw      string
			id       string
			noAuth   bool
			svcErr   error
			wantErr  string
			wantHTTP int
		}{
			{name: "missing tenant", body: map[string]any{"verdict": "wip"}, id: "mem-1", noAuth: true, wantErr: "Tenant ID"},
			{name: "missing id", body: map[string]any{"verdict": "wip"}, wantErr: "Memory ID"},
			{name: "invalid json", raw: "{", id: "mem-1", wantErr: "Invalid request"},
			{name: "stale write", body: map[string]any{"content": "new text here"}, id: "mem-1", svcErr: &types.MemoryV2ConflictError{Message: "the memory was changed by someone else"}, wantErr: "changed by someone else", wantHTTP: http.StatusConflict},
			{name: "empty patch", body: map[string]any{}, id: "mem-1", wantErr: "No updatable fields", wantHTTP: http.StatusBadRequest},
			{name: "validation", body: map[string]any{"importance": 99}, id: "mem-1", svcErr: &types.ErrMemoryValidation{Message: "importance must be between -5 and 6"}, wantErr: "importance must be", wantHTTP: http.StatusBadRequest},
			{name: "protected verdict", body: map[string]any{"verdict": "none"}, id: "mem-1", svcErr: &types.ErrProtectedVerdict{}, wantErr: "protected verdict", wantHTTP: http.StatusConflict},
			{name: "service error", body: map[string]any{"verdict": "wip"}, id: "mem-1", svcErr: errors.New("db down"), wantErr: "Failed to update memory", wantHTTP: http.StatusInternalServerError},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svc := &memoryV2HandlerServiceFake{patchErr: tt.svcErr}
				h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
				path := "/memories/" + tt.id
				var c *gin.Context
				if tt.raw != "" {
					c, _ = newMemoryV2HandlerRawContext(http.MethodPut, path, tt.raw)
				} else {
					c, _ = newMemoryV2HandlerTestContext(http.MethodPut, path, tt.body)
				}
				if tt.id != "" {
					c.Params = gin.Params{{Key: "id", Value: tt.id}}
				}
				if !tt.noAuth {
					setMemoryV2Auth(c, 42, "")
				}
				h.UpdateMemory(c)
				require.Len(t, c.Errors, 1)
				assert.Contains(t, c.Errors.Last().Error(), tt.wantErr)
				if tt.wantHTTP != 0 {
					var appErr *apperrors.AppError
					require.ErrorAs(t, c.Errors.Last().Err, &appErr)
					assert.Equal(t, tt.wantHTTP, appErr.HTTPCode)
				}
				assert.Zero(t, svc.saveCalls)
			})
		}
	})
}

func TestMemoryV2Handler_UpdateMemoryContentAndLock(t *testing.T) {
	svc := &memoryV2HandlerServiceFake{patchResult: &types.AgentMemory{ID: "mem-1"}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	c, recorder := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"content": "Kết nối postgres đã được tăng lên"})
	c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
	c.Request.Header.Set("If-Unmodified-Since", "2026-10-17T10:00:00.123456Z")
	setMemoryV2Auth(c, 42, "user-1")

	h.UpdateMemory(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, svc.patch.Content)
	assert.Equal(t, "Kết nối postgres đã được tăng lên", *svc.patch.Content)
	require.NotNil(t, svc.patch.ExpectedUpdatedAt, "If-Unmodified-Since becomes the optimistic lock")
	assert.Equal(t, 123456000, svc.patch.ExpectedUpdatedAt.Nanosecond())
}

func TestMemoryV2Handler_UpdateMemoryDuplicateConflictCarriesExistingID(t *testing.T) {
	svc := &memoryV2HandlerServiceFake{patchErr: &types.MemoryV2ConflictError{Message: "duplicate", ExistingID: "mem-9"}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	c, _ := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/mem-1", map[string]any{"content": "same as mem-9 text"})
	c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
	setMemoryV2Auth(c, 42, "")

	h.UpdateMemory(c)

	require.Len(t, c.Errors, 1)
	var appErr *apperrors.AppError
	require.ErrorAs(t, c.Errors.Last().Err, &appErr)
	assert.Equal(t, http.StatusConflict, appErr.HTTPCode)
	assert.Equal(t, gin.H{"existing_id": "mem-9"}, appErr.Details)
}

func TestMemoryV2Handler_DeleteMemory(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=%20%20", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Zero(t, svc.searchCalls)
	})

	t.Run("malformed optional numeric query defaults", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=hello&limit=bad&min_score=bad", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		assert.Empty(t, c.Errors)
		assert.Zero(t, svc.searchFilter.Limit)
		assert.Zero(t, svc.searchFilter.MinScore)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &memoryV2HandlerServiceFake{searchErr: errors.New("search failed")}
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/search?q=hello", nil)
		setMemoryV2Auth(c, 42, "")

		h.SearchMemories(c)

		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Failed to search memories")
	})
}

// graphRepoFake serves a small relation graph through the batched graph store:
// m1 -> m2 -> m3 (same KB) and m1 -> x (another KB, must never surface).
type graphRepoFake struct {
	*memoryV2HandlerRepoFake
	mems map[string]*types.AgentMemory
	rels []*types.MemoryRelation
}

func newGraphRepoFake() *graphRepoFake {
	mk := func(id, kb, content string, imp int, v types.MemoryVerdict) *types.AgentMemory {
		return &types.AgentMemory{ID: id, TenantID: "42", KbID: kb, Content: content, MemoryType: "fact", Importance: imp, Verdict: v, HubScore: 0.7, CreatedAt: time.Now()}
	}
	g := &graphRepoFake{mems: map[string]*types.AgentMemory{
		"m1": mk("m1", "kb1", "Postgres pool exhausted", 2, types.VerdictNone),
		"m2": mk("m2", "kb1", "Decided to use pgbouncer", 4, types.VerdictDecision),
		"m3": mk("m3", "kb1", strings.Repeat("Kết nối ", 30), 1, types.VerdictNone),
		"x":  mk("x", "kb-other", "other knowledge base", 1, types.VerdictNone),
	}}
	g.rels = []*types.MemoryRelation{
		{ID: "r1", TenantID: "42", FromUUID: "m1", ToUUID: "m2", RelationType: "related_to", Weight: 0.9},
		{ID: "r2", TenantID: "42", FromUUID: "m2", ToUUID: "m3", RelationType: "justifies", Weight: 0.8},
		{ID: "r3", TenantID: "42", FromUUID: "m1", ToUUID: "x", RelationType: "related_to", Weight: 0.99},
	}
	g.memoryV2HandlerRepoFake = &memoryV2HandlerRepoFake{getMemory: g.mems["m1"]}
	return g
}

func (g *graphRepoFake) GetByIDs(_ context.Context, _ string, ids []string) ([]*types.AgentMemory, error) {
	var out []*types.AgentMemory
	for _, id := range ids {
		if m, ok := g.mems[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (g *graphRepoFake) RelationsOf(_ context.Context, _ string, ids []string, _ int) ([]*types.MemoryRelation, error) {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	var out []*types.MemoryRelation
	for _, r := range g.rels {
		if set[r.FromUUID] || set[r.ToUUID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (g *graphRepoFake) TopHubs(_ context.Context, _, kbID string, _ int) ([]*types.AgentMemory, error) {
	var out []*types.AgentMemory
	for _, id := range []string{"m1", "m2", "m3", "x"} {
		if g.mems[id].KbID == kbID {
			out = append(out, g.mems[id])
		}
	}
	return out, nil
}

func graphRequest(t *testing.T, h *MemoryV2Handler, path string, id string) (map[string]any, *gin.Context) {
	t.Helper()
	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, path, nil)
	if id != "" {
		c.Params = gin.Params{{Key: "id", Value: id}}
		h.GetMemoryGraph(c)
	} else {
		setMemoryV2Auth(c, 42, "")
		h.GetMemoryGraphOverview(c)
		if len(c.Errors) > 0 {
			return nil, c
		}
		return jsonMap(t, decodeJSONBody(t, rec)["data"]), c
	}
	if len(c.Errors) > 0 {
		return nil, c
	}
	return jsonMap(t, decodeJSONBody(t, rec)["data"]), c
}

func nodeIDs(t *testing.T, data map[string]any) []string {
	var out []string
	for _, n := range jsonSlice(t, data["nodes"]) {
		out = append(out, jsonMap(t, n)["id"].(string))
	}
	return out
}

func TestMemoryV2Handler_GetMemoryGraph(t *testing.T) {
	t.Run("depth walks hops inside the knowledge base", func(t *testing.T) {
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, newGraphRepoFake(), nil, nil)
		c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/m1?depth=1", nil)
		c.Params = gin.Params{{Key: "id", Value: "m1"}}
		setMemoryV2Auth(c, 42, "")
		h.GetMemoryGraph(c)
		require.Equal(t, http.StatusOK, rec.Code)
		one := jsonMap(t, decodeJSONBody(t, rec)["data"])
		assert.ElementsMatch(t, []string{"m1", "m2"}, nodeIDs(t, one), "1 hop; the other-KB neighbour never surfaces")

		c, rec = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/m1?depth=2", nil)
		c.Params = gin.Params{{Key: "id", Value: "m1"}}
		setMemoryV2Auth(c, 42, "")
		h.GetMemoryGraph(c)
		two := jsonMap(t, decodeJSONBody(t, rec)["data"])
		assert.ElementsMatch(t, []string{"m1", "m2", "m3"}, nodeIDs(t, two))
		assert.Len(t, jsonSlice(t, two["edges"]), 2)
	})

	t.Run("wire format matches the web UI GraphNode/GraphEdge", func(t *testing.T) {
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, newGraphRepoFake(), nil, nil)
		c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/m1?depth=2", nil)
		c.Params = gin.Params{{Key: "id", Value: "m1"}}
		setMemoryV2Auth(c, 42, "")
		h.GetMemoryGraph(c)
		data := jsonMap(t, decodeJSONBody(t, rec)["data"])
		for _, raw := range jsonSlice(t, data["nodes"]) {
			n := jsonMap(t, raw)
			for _, k := range []string{"id", "label", "type", "importance", "verdict", "hub_score", "is_stale"} {
				assert.Contains(t, n, k, "node field %s", k)
			}
			assert.IsType(t, float64(0), n["importance"], "numeric importance (NaN radius before)")
			assert.LessOrEqual(t, utf8.RuneCountInString(n["label"].(string)), 80)
		}
		for _, raw := range jsonSlice(t, data["edges"]) {
			e := jsonMap(t, raw)
			for _, k := range []string{"source", "target", "relation_type", "weight"} {
				assert.Contains(t, e, k, "edge field %s", k)
			}
			assert.IsType(t, float64(0), e["weight"])
		}
	})

	t.Run("missing memory", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{getErr: errors.New("missing")}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/graph/mem-1", nil)
		c.Params = gin.Params{{Key: "id", Value: "mem-1"}}
		setMemoryV2Auth(c, 42, "")
		h.GetMemoryGraph(c)
		require.Len(t, c.Errors, 1)
		assert.Contains(t, c.Errors.Last().Error(), "Memory not found")
	})
}

func TestMemoryV2Handler_GetMemoryGraphOverview(t *testing.T) {
	h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, newGraphRepoFake(), nil, nil)
	data, c := graphRequest(t, h, "/memories/graph?kb_id=kb1", "")
	require.Empty(t, c.Errors)
	assert.ElementsMatch(t, []string{"m1", "m2", "m3"}, nodeIDs(t, data))
	assert.Len(t, jsonSlice(t, data["edges"]), 2, "only edges between overview nodes")

	_, c = graphRequest(t, h, "/memories/graph", "")
	require.Len(t, c.Errors, 1, "kb_id is required")
}

func TestMemoryV2Handler_GetMemoryStats(t *testing.T) {
	t.Run("happy path documents per type filters", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchTotals: []int64{10, 4, 3, 0}}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/stats?kb_id=kb1", nil)
		setMemoryV2Auth(c, 42, "")

		h.GetMemoryStats(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Len(t, repo.searchFilters, 4)
		assert.Equal(t, "kb1", repo.searchFilters[0].KbID)
		assert.Equal(t, "kb1", repo.searchFilters[1].KbID, "per-type counts stay inside the selected knowledge base")
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, nil, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, "v2", body["backend"])
		assert.Equal(t, false, body["available"])
	})

	t.Run("missing tenant", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, "v2", body["backend"])
		assert.Equal(t, true, body["available"])
		assert.Zero(t, repo.searchCalls)
	})

	t.Run("repo search success", func(t *testing.T) {
		repo := &memoryV2HandlerRepoFake{searchTotal: 12}
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
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
		h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, repo, nil, nil)
		c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/tenants/memory-status", nil)
		setMemoryV2Auth(c, 42, "")

		h.MemoryStatus(c)

		body := decodeJSONBody(t, recorder)
		assert.Equal(t, true, body["available"])
		assert.NotContains(t, body, "memory_count")
	})
}
