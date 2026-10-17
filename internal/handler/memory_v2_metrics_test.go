package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryMetrics_SystemAdminOnly(t *testing.T) {
	telemetry.WorkerRun("dreamer", "completed", time.Now())
	telemetry.WorkerActions("dreamer", "blocked_validation", 1)
	telemetry.DreamTokens(42)
	telemetry.Observe("recall_embedding", "model", time.Now())
	telemetry.Observe("recall_keyword", "database", time.Now())
	for _, systemAdmin := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/api/v1/memories/metrics", nil)
		ctx := context.WithValue(c.Request.Context(), types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleOwner)
		c.Request = c.Request.WithContext(ctx)
		(&MemoryV2Handler{}).MemoryMetrics(c)
		if !systemAdmin {
			require.Equal(t, 403, rec.Code)
			continue
		}
		require.Equal(t, 200, rec.Code)
		for _, metric := range []string{"worker_runs_total", "worker_actions_total", "dreamer_estimated_tokens_total", "worker_last_run_timestamp_seconds", "phase=\"model\"", "phase=\"database\""} {
			require.Contains(t, rec.Body.String(), metric)
		}
		require.NotContains(t, rec.Body.String(), "tenant_id")
	}
}
