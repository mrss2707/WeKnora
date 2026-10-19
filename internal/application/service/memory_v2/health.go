package memory_v2

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2HealthReporter = (*MemoryServiceV2Impl)(nil)

// AssessHealthReport keeps the same trusted scope contract as AssessHealth,
// while retaining bounded coverage and unavailable-check information.
func (s *MemoryServiceV2Impl) AssessHealthReport(ctx context.Context, tenantID, kbID string) (*types.HealthReport, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2: %s", reason)
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("memory V2: tenant ID is required for health assessment")
	}
	if s.healthChecker == nil {
		return nil, fmt.Errorf("memory V2: health checker unavailable")
	}
	var dimension int
	if embedder, _, err := s.embedderFor(ctx, tenantID, kbID); err == nil && embedder != nil {
		dimension = embedder.GetDimensions()
	}
	report, err := s.healthChecker.AssessHealth(ctx, tenantID, kbID, dimension)
	if err != nil {
		return nil, err
	}
	s.checkDreamerModel(ctx, report, tenantID, kbID)
	return report, nil
}

// dreamerModelIssue is reported when a scheduled Dreamer cannot run because
// its model is unusable. Skipped passes record no run, so without this check
// nothing would tell an administrator that the Dreamer stopped.
const dreamerModelIssue = "dreamer_model_unavailable"

// checkDreamerModel resolves the knowledge base's Dreamer model now, exactly
// as the next pass would, and reports a failure as a high-severity issue.
func (s *MemoryServiceV2Impl) checkDreamerModel(ctx context.Context, report *types.HealthReport, tenantID, kbID string) {
	if report == nil || kbID == "" || !s.config.Dreamer.Enabled || s.resolver == nil || s.chat != nil {
		return
	}
	if report.Checks == nil {
		report.Checks = map[string]types.MemoryHealthCheck{}
	}
	if report.BySeverity == nil {
		report.BySeverity = map[string]int{}
	}
	effective := s.effectiveModel(ctx, tenantID, kbID, types.MemoryModelPurposeDreamer)
	report.Checks["dreamer_model"] = types.MemoryHealthCheck{Status: "completed", Examined: 1}
	if effective.Source != types.MemoryModelSourceUnavailable {
		return
	}
	description := "The Dreamer has no usable chat model for this knowledge base, so its passes are skipped"
	if effective.ModelID != "" {
		description = "The Dreamer model chosen for this knowledge base is deleted, inactive or not a chat model, so its passes are skipped"
	}
	report.Issues = append(report.Issues, &types.MemoryHealthIssue{
		Type: dreamerModelIssue, Description: description, Severity: "high",
		Suggestion: "Choose an active chat model in Memory → Dreamer → Models, or clear the choice to use the knowledge base summary model",
	})
	report.BySeverity["high"]++
	report.TotalIssues = len(report.Issues)
}
