package workers

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const healthScanLimit = 1000
const healthIssueLimit = 500

func nextHealthRun(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())
	if !now.Before(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (h *HealthChecker) runAllChecks(ctx context.Context, tenantID, kbID string, embedderDim int) (*types.HealthReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results, total, err := h.repo.Search(ctx, &types.MemoryFilter{TenantID: tenantID, KbID: kbID, Limit: healthScanLimit})
	if err != nil {
		return nil, fmt.Errorf("health scan failed: %w", err)
	}
	report := &types.HealthReport{
		TenantID: tenantID, CheckedAt: time.Now(), BySeverity: map[string]int{},
		Coverage: &types.MemoryHealthCoverage{Total: total, Limit: healthScanLimit, Complete: true, Reasons: []string{}},
		Checks:   map[string]types.MemoryHealthCheck{},
	}
	partial := func(reason string) {
		report.Coverage.Complete = false
		if !slices.Contains(report.Coverage.Reasons, reason) {
			report.Coverage.Reasons = append(report.Coverage.Reasons, reason)
		}
	}
	if len(results) > healthScanLimit {
		results = results[:healthScanLimit]
		partial("scan_limit")
	}
	memories := make([]*types.AgentMemory, 0, len(results))
	for _, result := range results {
		if result == nil || result.Memory == nil {
			continue
		}
		memory := result.Memory
		if memory.DeletedAt.Valid || memory.TenantID != tenantID || (kbID != "" && memory.KbID != kbID) {
			continue
		}
		memories = append(memories, memory)
	}
	report.Coverage.Scanned = len(memories)
	if total < int64(len(memories)) {
		partial("inconsistent_count")
	}
	if int64(len(memories)) < total {
		partial("scan_limit")
	}
	if len(memories) != len(results) {
		partial("filtered_results")
	}
	add := func(issues []*types.MemoryHealthIssue) {
		remaining := healthIssueLimit - len(report.Issues)
		if len(issues) > remaining {
			issues = issues[:remaining]
			partial("issue_limit")
		}
		report.Issues = append(report.Issues, issues...)
	}
	checks := []struct {
		name string
		run  func([]*types.AgentMemory) []*types.MemoryHealthIssue
	}{
		{"orphans", h.checkOrphans}, {"stale", h.checkStaleFacts},
		{"duplication", h.checkDuplications}, {"graph_fragmentation", h.checkGraphFragmentation},
		{"verdict_consistency", h.checkVerdictConsistency},
	}
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		add(check.run(memories))
		report.Checks[check.name] = types.MemoryHealthCheck{Status: "completed", Examined: len(memories)}
	}
	candidates, examined, err := contradictionCandidates(ctx, memories)
	if err != nil {
		return nil, err
	}
	add(candidates)
	status := types.MemoryHealthCheck{Status: "completed", Examined: examined}
	if examined < len(memories) {
		status.Status, status.Reason = "partial", "unsupported_assertions"
	}
	report.Checks["contradiction"] = status
	report.Checks["embedding_dimension"] = types.MemoryHealthCheck{Status: "unavailable", Reason: "model_unavailable"}
	if len(memories) > 0 && embedderDim > 0 {
		var storedDim int
		if reader, ok := h.repo.(interfaces.MemoryV2HealthEmbeddingReader); ok {
			storedDim, err = reader.GetHealthEmbeddingDimension(ctx, tenantID, kbID)
		} else if kbID == "" {
			storedDim, err = h.repo.GetEmbeddingDimension(ctx, tenantID)
		} else {
			err = fmt.Errorf("scoped embedding inspection unavailable")
		}
		switch {
		case err != nil:
			report.Checks["embedding_dimension"] = types.MemoryHealthCheck{Status: "unavailable", Reason: "embedding_check_failed"}
		case storedDim <= 0:
			report.Checks["embedding_dimension"] = types.MemoryHealthCheck{Status: "unavailable", Reason: "no_embeddings"}
		default:
			add(h.checkEmbeddingDimension(storedDim, embedderDim))
			report.Checks["embedding_dimension"] = types.MemoryHealthCheck{Status: "completed", Examined: 1, Reason: "sampled_dimension"}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, issue := range report.Issues {
		report.BySeverity[issue.Severity]++
	}
	report.TotalIssues = len(report.Issues)
	return report, nil
}
