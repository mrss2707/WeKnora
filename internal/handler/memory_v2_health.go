package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// writeHealthReport runs only after the existing KB Viewer authorization gate.
func (h *MemoryV2Handler) writeHealthReport(c *gin.Context, ctx context.Context, tenantID, kbID string) {
	var report *types.HealthReport
	var err error
	if reporter, ok := h.memorySvc.(interfaces.MemoryV2HealthReporter); ok {
		report, err = reporter.AssessHealthReport(ctx, tenantID, kbID)
	} else {
		var issues []*types.MemoryHealthIssue
		issues, err = h.memorySvc.AssessHealth(ctx, tenantID, kbID)
		report = &types.HealthReport{
			TenantID: tenantID, CheckedAt: time.Now(), Issues: issues, TotalIssues: len(issues),
			BySeverity: map[string]int{},
			Coverage:   &types.MemoryHealthCoverage{Reasons: []string{"coverage_unknown"}},
		}
		for _, issue := range issues {
			report.BySeverity[issue.Severity]++
		}
	}
	if err == nil && (report == nil || report.TenantID != tenantID) {
		err = fmt.Errorf("health report unavailable in requested scope")
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"operation": "memory_health"})
		c.Error(errors.NewInternalServerError("Failed to assess health"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}
