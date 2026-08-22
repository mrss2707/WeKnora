package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

// memoryV2RouteCases is the single source of truth for the Memory V2 surface:
// every route exactly once, with its role floor. Concrete request paths are
// used for the HTTP probes (":id" is the registered template).
var memoryV2RouteCases = []struct {
	method     string
	path       string // registered template
	probePath  string // concrete path for HTTP probes
	roleFloor  types.TenantRole
}{
	{http.MethodGet, "/api/v1/memories", "/api/v1/memories", types.TenantRoleViewer},
	{http.MethodGet, "/api/v1/memories/search", "/api/v1/memories/search", types.TenantRoleViewer},
	{http.MethodGet, "/api/v1/memories/stats", "/api/v1/memories/stats", types.TenantRoleViewer},
	{http.MethodGet, "/api/v1/memories/health", "/api/v1/memories/health", types.TenantRoleViewer},
	{http.MethodGet, "/api/v1/memories/graph/:id", "/api/v1/memories/graph/m1", types.TenantRoleViewer},
	{http.MethodGet, "/api/v1/memories/:id", "/api/v1/memories/m1", types.TenantRoleViewer},
	{http.MethodPost, "/api/v1/memories", "/api/v1/memories", types.TenantRoleContributor},
	{http.MethodPut, "/api/v1/memories/:id", "/api/v1/memories/m1", types.TenantRoleContributor},
	{http.MethodDelete, "/api/v1/memories/:id", "/api/v1/memories/m1", types.TenantRoleContributor},
	{http.MethodPost, "/api/v1/memories/dream", "/api/v1/memories/dream", types.TenantRoleAdmin},
	{http.MethodGet, "/api/v1/tenants/memory-status", "/api/v1/tenants/memory-status", types.TenantRoleViewer},
}

// rbacEnforcingGuards returns guards with role enforcement switched on
// (EnableRBAC defaults true via TenantConfig.IsRBACEnforced).
func rbacEnforcingGuards() *rbacGuards {
	return &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{}}}
}

// roleDrivingEngine builds an engine where the caller role is injected from a
// header, then registers the real Memory V2 routes on top, so the true
// RequireRole middleware chain decides each probe.
func roleDrivingEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		role := types.TenantRole(c.GetHeader("X-Test-Role"))
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), types.TenantRoleContextKey, role),
		)
		c.Next()
	})
	RegisterMemoryV2Routes(v1, &handler.MemoryV2Handler{}, rbacEnforcingGuards())
	return engine
}

func probeRoute(t *testing.T, engine *gin.Engine, method, path, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Test-Role", role)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestMemoryV2RouteRoleFloors drives every route with roles below, at and
// above its floor: below must yield 403 with the RBAC rejection message; at
// or above must pass the gate (the empty test handler then answers on its
// own, never via the role middleware 403).
func TestMemoryV2RouteRoleFloors(t *testing.T) {
	engine := roleDrivingEngine(t)

	// Any role strictly below the floor must be rejected with the RBAC
	// 403 and the named "insufficient workspace role" error.
	below := map[types.TenantRole][]types.TenantRole{
		types.TenantRoleViewer:      {}, // Viewer is the lowest floor: no rejectable valid role
		types.TenantRoleContributor: {types.TenantRoleViewer},
		types.TenantRoleAdmin:       {types.TenantRoleViewer, types.TenantRoleContributor},
	}
	for _, tc := range memoryV2RouteCases {
		for _, role := range below[tc.roleFloor] {
			t.Run(tc.method+" "+tc.path+" as "+string(role)+" (denied)", func(t *testing.T) {
				rec := probeRoute(t, engine, tc.method, tc.probePath, string(role))
				if rec.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403", rec.Code)
				}
				if !strings.Contains(rec.Body.String(), "Forbidden: insufficient workspace role") {
					t.Fatalf("body = %q, want the RBAC rejection error", rec.Body.String())
				}
			})
		}
	}

	// At the floor and above, the role gate must let the request through
	// (the guard's own response is the only 403 source on these routes).
	atFloor := map[types.TenantRole][]types.TenantRole{
		types.TenantRoleViewer:      {types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin},
		types.TenantRoleContributor: {types.TenantRoleContributor, types.TenantRoleAdmin},
		types.TenantRoleAdmin:       {types.TenantRoleAdmin},
	}
	for _, tc := range memoryV2RouteCases {
		for _, role := range atFloor[tc.roleFloor] {
			t.Run(tc.method+" "+tc.path+" as "+string(role)+" (allowed)", func(t *testing.T) {
				rec := probeRoute(t, engine, tc.method, tc.probePath, string(role))
				if rec.Code == http.StatusForbidden &&
					strings.Contains(rec.Body.String(), "insufficient workspace role") {
					t.Fatalf("role %s was rejected on a %s-floor route", role, tc.roleFloor)
				}
			})
		}
	}
}

// TestMemoryV2RoutesCompleteAndUnique proves every expected route is
// registered exactly once and nothing extra slipped in.
func TestMemoryV2RoutesCompleteAndUnique(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterMemoryV2Routes(v1, &handler.MemoryV2Handler{}, &rbacGuards{})

	got := map[string]int{}
	for _, r := range engine.Routes() {
		got[r.Method+"|"+r.Path]++
	}
	if len(got) != len(memoryV2RouteCases) {
		t.Fatalf("registered routes = %#v, want %d routes", got, len(memoryV2RouteCases))
	}
	for _, tc := range memoryV2RouteCases {
		if got[tc.method+"|"+tc.path] != 1 {
			t.Fatalf("route %s %s seen %d times, want exactly 1", tc.method, tc.path, got[tc.method+"|"+tc.path])
		}
	}
}

// TestMemoryV2RoutesNilHandlerNoOp keeps the caller-side nil guarantee: with
// a nil handler, no route may be registered (V2 disabled = surface absent).
func TestMemoryV2RoutesNilHandlerNoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterMemoryV2Routes(v1, nil, &rbacGuards{})
	if len(engine.Routes()) != 0 {
		t.Fatalf("nil handler registered routes: %#v", engine.Routes())
	}
}

// TestMemoryV2RoutesDefaultDenyAPIKeys locks the current policy contract:
// machine principals (API keys) are default-denied on every Memory V2 route
// because no capability policy grants them access.
func TestMemoryV2RoutesDefaultDenyAPIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{}
	g.ensureAPIKeyAuthorizer()
	v1 := gin.New().Group("/api/v1")
	RegisterMemoryV2Routes(v1, &handler.MemoryV2Handler{}, g)

	for _, tc := range memoryV2RouteCases {
		if _, ok := g.apiKeyAuthorizer.Lookup(tc.method, tc.path); ok {
			t.Fatalf("route %s %s has an API-key policy; Memory V2 routes must stay default-deny", tc.method, tc.path)
		}
	}
}