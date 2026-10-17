package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// dreamServiceFake adds the optional preview and status capabilities.
type dreamServiceFake struct {
	*memoryV2HandlerServiceFake
	previewKB string
	status    *types.DreamStatus
}

func (f *dreamServiceFake) PreviewDreamScope(_ context.Context, tenantID, kbID string) (*types.DreamResult, error) {
	f.previewKB = tenantID + "/" + kbID
	return &types.DreamResult{ActionsProposed: 1, Actions: []types.DreamAction{{Type: "update_verdict", TargetID: "m1"}}}, nil
}

func (f *dreamServiceFake) DreamStatus(_ context.Context, tenantID, kbID string) (*types.DreamStatus, error) {
	return f.status, nil
}

func TestMemoryV2Handler_TriggerDreamDryRunPreviews(t *testing.T) {
	_, repo, _, _ := newScopeHandler(types.OrgRoleAdmin, true)
	svc := &dreamServiceFake{memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{}}
	h := NewMemoryV2Handler(svc, repo, (&scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-own": {ID: "kb-own", TenantID: 42},
	}}), &scopeKBShares{roles: map[string]types.OrgMemberRole{}})
	c, rec := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", map[string]any{"kb_id": "kb-own", "dry_run": true})
	setMemoryV2Auth(c, 42, "alice")

	h.TriggerDream(c)

	require.Equal(t, http.StatusOK, rec.Code, c.Errors.String())
	assert.Equal(t, "42/kb-own", svc.previewKB)
	assert.Zero(t, svc.dreamCalls, "a dry run never runs a real pass")
	data := jsonMap(t, decodeJSONBody(t, rec)["data"])
	assert.Equal(t, float64(1), data["actions_proposed"])
}

func TestMemoryV2Handler_TriggerDreamDryRunNeedsKB(t *testing.T) {
	svc := &dreamServiceFake{memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", map[string]any{"dry_run": true})
	setMemoryV2Auth(c, 42, "alice")

	h.TriggerDream(c)

	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "kb_id")
}

func TestMemoryV2Handler_GetDreamStatus(t *testing.T) {
	last := time.Now().Add(-time.Hour)
	svc := &dreamServiceFake{
		memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{},
		status:                     &types.DreamStatus{Enabled: true, Scheduled: true, Runs: 4, LastRunAt: &last, MergeLive: true},
	}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, &scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-own": {ID: "kb-own", TenantID: 42},
	}}, &scopeKBShares{roles: map[string]types.OrgMemberRole{}})

	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/dream?kb_id=kb-own", nil)
	setMemoryV2Auth(c, 42, "dave")
	h.GetDreamStatus(c)
	require.Equal(t, http.StatusOK, rec.Code, c.Errors.String())
	data := jsonMap(t, decodeJSONBody(t, rec)["data"])
	assert.Equal(t, float64(4), data["runs"])
	assert.Equal(t, true, data["merge_live"])

	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/dream", nil)
	setMemoryV2Auth(c, 42, "dave")
	h.GetDreamStatus(c)
	require.Len(t, c.Errors, 1, "kb_id is required")
}
