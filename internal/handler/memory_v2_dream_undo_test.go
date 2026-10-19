package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type dreamUndoServiceFake struct {
	*memoryV2HandlerServiceFake
	scope, run string
}

func (f *dreamUndoServiceFake) UndoDreamRun(_ context.Context, tenantID, kbID, runID string) (*types.DreamUndoResult, error) {
	f.scope, f.run = tenantID+"/"+kbID, runID
	return &types.DreamUndoResult{RunID: runID, Reverted: 2, Skipped: []types.DreamUndoSkip{}}, nil
}

func newDreamUndoHandler(role types.OrgMemberRole) (*MemoryV2Handler, *dreamUndoServiceFake) {
	svc := &dreamUndoServiceFake{memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, &scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-own": {ID: "kb-own", TenantID: 42}, "kb-shared": {ID: "kb-shared", TenantID: 7},
	}}, &scopeKBShares{roles: map[string]types.OrgMemberRole{"kb-shared": role}})
	return h, svc
}

func TestUndoDreamRun_ScopesToOwnerTenant(t *testing.T) {
	h, svc := newDreamUndoHandler(types.OrgRoleAdmin)
	c, rec := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream/undo", map[string]any{"kb_id": "kb-shared", "run_id": "run-1"})
	setMemoryV2Auth(c, 42, "alice")
	h.UndoDreamRun(c)
	require.Equal(t, http.StatusOK, rec.Code, c.Errors.String())
	assert.Equal(t, "7/kb-shared", svc.scope, "a shared KB's pass is undone in the owner tenant")
	assert.Equal(t, "run-1", svc.run)
	data := jsonMap(t, decodeJSONBody(t, rec)["data"])
	assert.Equal(t, float64(2), data["reverted"])
}

func TestUndoDreamRun_RequiresAdminAndIDs(t *testing.T) {
	h, svc := newDreamUndoHandler(types.OrgRoleViewer)
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream/undo", map[string]any{"kb_id": "kb-shared", "run_id": "run-1"})
	setMemoryV2Auth(c, 42, "alice")
	h.UndoDreamRun(c)
	require.NotEmpty(t, c.Errors, "a viewer of a shared KB cannot undo its passes")
	assert.Empty(t, svc.scope)

	for _, body := range []map[string]any{{"kb_id": "kb-own"}, {"run_id": "run-1"}} {
		c, _ = newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream/undo", body)
		setMemoryV2Auth(c, 42, "alice")
		h.UndoDreamRun(c)
		require.Len(t, c.Errors, 1)
	}
	assert.Empty(t, svc.scope)
}

func TestUndoDreamRun_LegacyServiceUnavailable(t *testing.T) {
	h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, &memoryV2HandlerRepoFake{}, &scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-own": {ID: "kb-own", TenantID: 42},
	}}, &scopeKBShares{roles: map[string]types.OrgMemberRole{}})
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream/undo", map[string]any{"kb_id": "kb-own", "run_id": "run-1"})
	setMemoryV2Auth(c, 42, "alice")
	h.UndoDreamRun(c)
	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "not available")
}
