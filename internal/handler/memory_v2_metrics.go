package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MemoryMetrics exposes content-free process metrics only to system admins.
// A tenant admin must not learn global activity, even with RBAC rollout off.
func (h *MemoryV2Handler) MemoryMetrics(c *gin.Context) {
	if !types.IsSystemAdminFromContext(c.Request.Context()) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "system administrator required"})
		return
	}
	promhttp.HandlerFor(telemetry.Registry, promhttp.HandlerOpts{}).ServeHTTP(c.Writer, c.Request)
}
