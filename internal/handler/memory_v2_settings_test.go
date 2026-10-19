package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type settingsServiceFake struct {
	*memoryV2HandlerServiceFake
	scope, user string
	update      types.MemoryV2ModelSettingsUpdate
	err         error
	dreamErr    error
}

func (f *settingsServiceFake) GetModelSettings(_ context.Context, tenantID, kbID string) (*types.MemoryV2ModelSettings, error) {
	f.scope = tenantID + "/" + kbID
	return &types.MemoryV2ModelSettings{KbID: kbID, Dreamer: types.MemoryEffectiveModel{ModelID: "pro", Source: types.MemoryModelSourceSetting}}, f.err
}

func (f *settingsServiceFake) UpdateModelSettings(_ context.Context, tenantID, kbID, userID string, u types.MemoryV2ModelSettingsUpdate) (*types.MemoryV2ModelSettings, error) {
	f.scope, f.user, f.update = tenantID+"/"+kbID, userID, u
	return &types.MemoryV2ModelSettings{KbID: kbID, DreamerModelID: u.DreamerModelID}, f.err
}

func (f *settingsServiceFake) ConsolidateDreamScope(_ context.Context, _, _ string) (*types.DreamResult, error) {
	return nil, f.dreamErr
}

func newSettingsHandler(role types.OrgMemberRole) (*MemoryV2Handler, *settingsServiceFake) {
	svc := &settingsServiceFake{memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, &scopeKBService{kbs: map[string]*types.KnowledgeBase{
		"kb-own": {ID: "kb-own", TenantID: 42}, "kb-shared": {ID: "kb-shared", TenantID: 7},
	}}, &scopeKBShares{roles: map[string]types.OrgMemberRole{"kb-shared": role}})
	return h, svc
}

func TestMemoryModelSettings_RequireKB(t *testing.T) {
	h, svc := newSettingsHandler(types.OrgRoleAdmin)
	c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/settings", nil)
	setMemoryV2Auth(c, 42, "alice")
	h.GetMemoryModelSettings(c)
	require.Len(t, c.Errors, 1)
	c, _ = newMemoryV2HandlerTestContext(http.MethodPut, "/memories/settings", map[string]any{"dreamer_model_id": "pro"})
	setMemoryV2Auth(c, 42, "alice")
	h.UpdateMemoryModelSettings(c)
	require.Len(t, c.Errors, 1)
	assert.Empty(t, svc.scope)
}

func TestMemoryModelSettings_SharedKBUsesOwnerTenantAndAdminFloor(t *testing.T) {
	h, svc := newSettingsHandler(types.OrgRoleViewer)
	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/settings?kb_id=kb-shared", nil)
	setMemoryV2Auth(c, 42, "alice")
	h.GetMemoryModelSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, c.Errors.String())
	assert.Equal(t, "7/kb-shared", svc.scope, "a shared KB's settings belong to the owner tenant")

	svc.scope = ""
	c, _ = newMemoryV2HandlerTestContext(http.MethodPut, "/memories/settings", map[string]any{"kb_id": "kb-shared", "dreamer_model_id": "pro"})
	setMemoryV2Auth(c, 42, "alice")
	h.UpdateMemoryModelSettings(c)
	require.NotEmpty(t, c.Errors, "a viewer of a shared KB cannot change its models")
	assert.Empty(t, svc.scope)

	h, svc = newSettingsHandler(types.OrgRoleAdmin)
	c, rec = newMemoryV2HandlerTestContext(http.MethodPut, "/memories/settings", map[string]any{"kb_id": "kb-shared", "dreamer_model_id": "pro", "text_model_id": "flash"})
	setMemoryV2Auth(c, 42, "alice")
	h.UpdateMemoryModelSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, c.Errors.String())
	assert.Equal(t, "7/kb-shared", svc.scope)
	assert.Equal(t, "alice", svc.user)
	assert.Equal(t, types.MemoryV2ModelSettingsUpdate{DreamerModelID: "pro", TextModelID: "flash"}, svc.update)
}

func TestMemoryModelSettings_ErrorsAreSafe(t *testing.T) {
	h, svc := newSettingsHandler(types.OrgRoleAdmin)
	svc.err = types.ErrMemoryModelSettingInvalid
	c, _ := newMemoryV2HandlerTestContext(http.MethodPut, "/memories/settings", map[string]any{"kb_id": "kb-own", "dreamer_model_id": "x"})
	setMemoryV2Auth(c, 42, "alice")
	h.UpdateMemoryModelSettings(c)
	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "active chat model")

	svc.err = errors.New("private SQL detail")
	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/settings?kb_id=kb-own", nil)
	setMemoryV2Auth(c, 42, "alice")
	h.GetMemoryModelSettings(c)
	require.Len(t, c.Errors, 1)
	assert.NotContains(t, c.Errors.Last().Error(), "private SQL")
}

func TestMemoryModelSettings_LegacyServiceNotImplemented(t *testing.T) {
	h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, &memoryV2HandlerRepoFake{}, nil, nil)
	c, rec := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/settings?kb_id=kb-own", nil)
	setMemoryV2Auth(c, 42, "alice")
	h.GetMemoryModelSettings(c)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestTriggerDream_UnavailableConfiguredModel(t *testing.T) {
	h, svc := newSettingsHandler(types.OrgRoleAdmin)
	svc.dreamErr = types.ErrMemoryModelUnavailable
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", map[string]any{"kb_id": "kb-own"})
	setMemoryV2Auth(c, 42, "alice")
	h.TriggerDream(c)
	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "Dreamer model configured")
}
