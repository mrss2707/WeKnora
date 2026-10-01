package middleware

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// apiKeyHeader is the header a client may send alongside the endpoint bearer
// token to name the human behind the call, using a Tenant API Key whose
// created_by records that human.
const apiKeyHeader = "X-API-Key"

// MCPEndpointUserAttribution resolves a per-user identity for MCP calls when the
// client also presents a Tenant API Key (X-API-Key) that a human created. It
// runs after MCPEndpointAuth (which established the endpoint's tenant, scope and
// a synthetic "mcp-<id>" machine user) and overrides ONLY the user identity so
// downstream writes (e.g. memory_save) can attribute them to a real person.
//
// Attribution only — it never rejects the request. An absent, invalid, revoked,
// cross-tenant or non-human-owned key simply leaves the synthetic machine user
// in place. The endpoint bearer token remains the authorization authority; the
// API key merely names the human.
func MCPEndpointUserAttribution(apiKeySvc interfaces.TenantAPIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer c.Next()
		if apiKeySvc == nil {
			return
		}
		raw := strings.TrimSpace(c.GetHeader(apiKeyHeader))
		if raw == "" {
			return
		}
		ctx := c.Request.Context()
		ep, ok := MCPEndpointFromContext(ctx)
		if !ok || ep == nil {
			return
		}
		key, err := apiKeySvc.AuthenticateAPIKey(ctx, raw)
		if err != nil || key == nil {
			logger.Debugf(ctx, "[mcp-endpoint] X-API-Key not usable for attribution: %v", err)
			return
		}
		// Accept only a key belonging to this workspace (platform keys may span
		// workspaces) that a human created.
		if !key.IsPlatform() && key.TenantIDValue() != ep.TenantID {
			return
		}
		humanID := strings.TrimSpace(key.CreatedBy)
		if !types.IsHumanUserID(humanID) {
			return
		}
		overrideAuthUser(c, &types.User{
			ID:       humanID,
			Username: humanID,
			TenantID: ep.TenantID,
			IsActive: true,
		}, ep.TenantID)
	}
}

// overrideAuthUser replaces the request's user identity (set by
// MCPEndpointAuth) with the given human user on both the gin keys and the
// request context, and refreshes the caller snapshot so audit trails see the
// human rather than the synthetic machine user.
func overrideAuthUser(c *gin.Context, user *types.User, tenantID uint64) {
	role := types.TenantRoleFromContext(c.Request.Context())
	c.Set(types.UserContextKey.String(), user)
	c.Set(types.UserIDContextKey.String(), user.ID)
	ctx := context.WithValue(c.Request.Context(), types.UserContextKey, user)
	ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: user.ID, Role: role})
	c.Request = c.Request.WithContext(ctx)
}
