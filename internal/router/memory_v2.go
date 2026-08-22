package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterMemoryV2Routes registers Memory V2 API routes.
// All routes are nil-safe: if the handler is nil (V2 disabled), the function
// is a no-op so callers don't need their own nil guard.
//
// Role floors: read/search/stats/health/graph/status = Viewer; create/update/
// delete = Contributor; dream trigger = Admin. API keys remain default-deny on
// these routes (no capability grants are declared), matching the rbacGuards
// contract where only explicitly registered policies are eligible.
func RegisterMemoryV2Routes(v1 *gin.RouterGroup, handler *handler.MemoryV2Handler, g *rbacGuards) {
	if handler == nil {
		return
	}
	memories := v1.Group("/memories")
	{
		memories.GET("", g.Viewer(), handler.ListMemories)
		memories.GET("/search", g.Viewer(), handler.SearchMemories)
		memories.GET("/stats", g.Viewer(), handler.GetMemoryStats)
		memories.GET("/health", g.Viewer(), handler.GetHealthReport)
		memories.GET("/graph/:id", g.Viewer(), handler.GetMemoryGraph)
		memories.GET("/:id", g.Viewer(), handler.GetMemory)
		memories.POST("", g.Contributor(), handler.CreateMemory)
		memories.PUT("/:id", g.Contributor(), handler.UpdateMemory)
		memories.DELETE("/:id", g.Contributor(), handler.DeleteMemory)
		memories.POST("/dream", g.Admin(), handler.TriggerDream)
	}
	v1.GET("/tenants/memory-status", g.Viewer(), handler.MemoryStatus)
}