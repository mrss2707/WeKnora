package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// scopeKBService serves knowledge bases by ID; the rest of the interface is
// intentionally unimplemented (embedded nil) because the handler never uses it.
type scopeKBService struct {
	interfaces.KnowledgeBaseService
	kbs map[string]*types.KnowledgeBase
}

func (s *scopeKBService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, repository.ErrKnowledgeBaseNotFound
}

// scopeKBShares grants the calling tenant a fixed role on shared KBs.
type scopeKBShares struct {
	interfaces.KBShareService
	roles map[string]types.OrgMemberRole
}

func (s *scopeKBShares) CheckTenantKBPermission(
	_ context.Context, kbID string, _ uint64, _ types.TenantRole,
) (types.OrgMemberRole, bool, error) {
	role, ok := s.roles[kbID]
	return role, ok, nil
}

// scopeRepoFake adds the optional by-ID locator to the standard repository fake.
type scopeRepoFake struct {
	*memoryV2HandlerRepoFake
	byID map[string]*types.AgentMemory
}

func (r *scopeRepoFake) FindByID(_ context.Context, id string) (*types.AgentMemory, error) {
	if m, ok := r.byID[id]; ok {
		return m, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (r *scopeRepoFake) ListKBScopeMismatches(context.Context, int) ([]*interfaces.MemoryKBScopeMismatch, error) {
	return nil, nil
}
func (r *scopeRepoFake) MoveToKBOwnerTenant(context.Context, *interfaces.MemoryKBScopeMismatch, string) error {
	return nil
}
func (r *scopeRepoFake) SyncRelationTenants(context.Context, []string) (int64, int64, error) {
	return 0, 0, nil
}

// Tenant 42 is the caller; the knowledge bases are owned by tenant 7.
func newScopeHandler(role types.OrgMemberRole, withShare bool) (*MemoryV2Handler, *memoryV2HandlerRepoFake, *memoryV2HandlerServiceFake, *scopeRepoFake) {
	base := &memoryV2HandlerRepoFake{}
	repo := &scopeRepoFake{memoryV2HandlerRepoFake: base, byID: map[string]*types.AgentMemory{
		"m-shared": {ID: "m-shared", TenantID: "7", KbID: "kb-shared", Content: "x", UserID: "alice"},
		"m-legacy": {ID: "m-legacy", TenantID: "42", KbID: "kb-shared", Content: "y", UserID: "bob"},
		"m-other":  {ID: "m-other", TenantID: "9", KbID: "kb-shared", Content: "z"},
	}}
	kbs := &scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-shared": {ID: "kb-shared", TenantID: 7},
		"kb-own":    {ID: "kb-own", TenantID: 42},
	}}
	shares := &scopeKBShares{roles: map[string]types.OrgMemberRole{}}
	if withShare {
		shares.roles["kb-shared"] = role
	}
	svc := &memoryV2HandlerServiceFake{}
	return NewMemoryV2Handler(svc, repo, kbs, shares), base, svc, repo
}

func TestMemoryV2Handler_SharedKBListsOwnerTenantMemories(t *testing.T) {
	h, base, _, _ := newScopeHandler(types.OrgRoleViewer, true)
	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories?kb_id=kb-shared&author=me", nil)
	setMemoryV2Auth(c, 42, "bob")

	h.ListMemories(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, base.searchFilters, 1)
	f := base.searchFilters[0]
	assert.Equal(t, "7", f.TenantID, "reads use the KB owner's tenant, not the caller's")
	assert.Equal(t, "kb-shared", f.KbID)
	assert.Equal(t, "bob", f.AuthorUserID, "author=me narrows to the caller only on request")
}

func TestMemoryV2Handler_ListDefaultsToAllMembersAndAcceptsUILegacyParams(t *testing.T) {
	h, base, _, _ := newScopeHandler(types.OrgRoleViewer, true)
	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories?kb_id=kb-own&type=semantic&verdict=confirmed&keyword=redis", nil)
	setMemoryV2Auth(c, 42, "bob")

	h.ListMemories(c)

	require.Equal(t, http.StatusOK, rec.Code)
	f := base.searchFilters[0]
	assert.Equal(t, "42", f.TenantID)
	assert.Empty(t, f.AuthorUserID, "by default every member's memories are listed")
	assert.Equal(t, "semantic", f.MemoryType)
	assert.Equal(t, "redis", f.Query)
	require.Len(t, f.Verdicts, 1)
	assert.Equal(t, types.MemoryVerdict("confirmed"), f.Verdicts[0])
}

func TestMemoryV2Handler_KBWithoutShareIsForbidden(t *testing.T) {
	h, base, _, _ := newScopeHandler(types.OrgRoleViewer, false)
	c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories?kb_id=kb-shared", nil)
	setMemoryV2Auth(c, 42, "bob")

	h.ListMemories(c)

	require.Len(t, c.Errors, 1)
	assert.Empty(t, base.searchFilters, "no repository access without a grant")
}

func TestMemoryV2Handler_SharedKBWriteNeedsEditor(t *testing.T) {
	body := map[string]any{"kb_id": "kb-shared", "content": "a shared fact"}

	h, _, svc, _ := newScopeHandler(types.OrgRoleViewer, true)
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", body)
	setMemoryV2Auth(c, 42, "bob")
	h.CreateMemory(c)
	require.Len(t, c.Errors, 1, "a viewer share cannot write")
	assert.Zero(t, svc.saveCalls)

	h, _, svc, _ = newScopeHandler(types.OrgRoleEditor, true)
	c, rec := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", body)
	setMemoryV2Auth(c, 42, "bob")
	h.CreateMemory(c)
	require.Empty(t, c.Errors)
	require.NotEqual(t, http.StatusForbidden, rec.Code)
	require.Equal(t, 1, svc.saveCalls)
	assert.Equal(t, "7", svc.saved.TenantID, "stored under the owner tenant so every member sees it")
	assert.Equal(t, "bob", svc.saved.UserID, "the author is still recorded")
}

func TestMemoryV2Handler_GetByIDResolvesTenantFromKB(t *testing.T) {
	h, base, _, _ := newScopeHandler(types.OrgRoleViewer, true)

	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/m-shared", nil)
	setMemoryV2Auth(c, 42, "bob")
	c.Params = gin.Params{{Key: "id", Value: "m-shared"}}
	h.GetMemory(c)
	require.Empty(t, c.Errors)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "7", base.getTenant)

	// A row still sitting in the writer's own tenant stays reachable until repaired.
	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/m-legacy", nil)
	setMemoryV2Auth(c, 42, "bob")
	c.Params = gin.Params{{Key: "id", Value: "m-legacy"}}
	h.GetMemory(c)
	require.Empty(t, c.Errors)
	assert.Equal(t, "42", base.getTenant)

	// A row in an unrelated tenant is invisible even though its KB is shared.
	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/m-other", nil)
	setMemoryV2Auth(c, 42, "bob")
	c.Params = gin.Params{{Key: "id", Value: "m-other"}}
	h.GetMemory(c)
	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "Memory not found")
}

func TestMemoryV2Handler_DeleteSharedMemoryNeedsEditor(t *testing.T) {
	h, base, _, _ := newScopeHandler(types.OrgRoleViewer, true)
	c, _ := newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/m-shared", nil)
	setMemoryV2Auth(c, 42, "bob")
	c.Params = gin.Params{{Key: "id", Value: "m-shared"}}
	h.DeleteMemory(c)
	require.Len(t, c.Errors, 1)
	assert.Zero(t, base.deleteCalls)

	h, base, _, _ = newScopeHandler(types.OrgRoleEditor, true)
	c, _ = newMemoryV2HandlerTestContext(http.MethodDelete, "/memories/m-shared", nil)
	setMemoryV2Auth(c, 42, "bob")
	c.Params = gin.Params{{Key: "id", Value: "m-shared"}}
	h.DeleteMemory(c)
	require.Empty(t, c.Errors)
	assert.Equal(t, "7", base.deleteTenant)
}
