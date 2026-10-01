package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAttributionKeySvc struct {
	interfaces.TenantAPIKeyService
	keys map[string]*types.TenantAPIKey
}

func (f *fakeAttributionKeySvc) AuthenticateAPIKey(_ context.Context, token string) (*types.TenantAPIKey, error) {
	if k, ok := f.keys[token]; ok {
		return k, nil
	}
	return nil, errors.New("not found")
}

func u64(v uint64) *uint64 { return &v }

// runAttribution drives MCPEndpointAuth + MCPEndpointUserAttribution and returns
// the user id seen by the final handler plus the HTTP status.
func runAttribution(t *testing.T, keys map[string]*types.TenantAPIKey, apiKeyHeader string) (string, int) {
	t.Helper()
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 42, Enabled: true}
	svc := &fakeMCPEndpointService{ep: ep, token: "mcp_secret"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.ContextWithFallback = true
	tenantSvc := &fakeTenantService{tenant: &types.Tenant{ID: 42, Status: "active"}}
	var seen string
	r.POST("/mcp/:endpoint_id",
		MCPEndpointAuth(svc, tenantSvc),
		MCPEndpointUserAttribution(&fakeAttributionKeySvc{keys: keys}),
		func(c *gin.Context) {
			seen, _ = types.UserIDFromContext(c.Request.Context())
			c.Status(http.StatusNoContent)
		})

	req := httptest.NewRequest(http.MethodPost, "/mcp/ep-1", nil)
	req.Header.Set("Authorization", "Bearer mcp_secret")
	if apiKeyHeader != "" {
		req.Header.Set("X-API-Key", apiKeyHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return seen, w.Code
}

func TestMCPUserAttributionOverridesWithKeyOwner(t *testing.T) {
	keys := map[string]*types.TenantAPIKey{
		"sk-dev": {TenantID: u64(42), CreatedBy: "550e8400-e29b-41d4-a716-446655440000"},
	}
	user, code := runAttribution(t, keys, "sk-dev")
	require.Equal(t, http.StatusNoContent, code)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", user)
}

func TestMCPUserAttributionFallsBackToSyntheticUser(t *testing.T) {
	keys := map[string]*types.TenantAPIKey{
		"sk-other-tenant": {TenantID: u64(7), CreatedBy: "550e8400-e29b-41d4-a716-446655440000"},
		"sk-machine":      {TenantID: u64(42), CreatedBy: ""},
		"sk-synthetic":    {TenantID: u64(42), CreatedBy: "system-42"},
	}
	cases := map[string]string{
		"no header":      "",
		"unknown key":    "sk-nope",
		"cross tenant":   "sk-other-tenant",
		"machine key":    "sk-machine",
		"synthetic user": "sk-synthetic",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			user, code := runAttribution(t, keys, header)
			// Attribution is best-effort: never a 401, user stays the machine user.
			require.Equal(t, http.StatusNoContent, code)
			assert.Equal(t, "mcp-ep-1", user)
		})
	}
}
