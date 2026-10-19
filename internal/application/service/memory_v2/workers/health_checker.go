package workers

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// HealthChecker runs periodic health checks on the memory system.
type HealthChecker struct {
	repo interfaces.MemoryRepositoryV2
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(repo interfaces.MemoryRepositoryV2) *HealthChecker {
	return &HealthChecker{repo: repo}
}

// Run starts the health checker worker loop. Runs daily at 4:00 AM.
func (h *HealthChecker) Run(ctx context.Context) {
	// Run once on startup
	h.runChecks(ctx)

	for {
		now := time.Now()
		delay := nextHealthRun(now).Sub(now)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			h.runChecks(ctx)
		}
	}
}

// AssessHealth runs all 7 health checks and returns the issues.
// This is the public entry point called from the HTTP handler.
func (h *HealthChecker) AssessHealth(ctx context.Context, tenantID, kbID string, embedderDim int) (*types.HealthReport, error) {
	return h.runAllChecks(ctx, tenantID, kbID, embedderDim)
}

// runChecks runs all health checks for every (tenant, knowledge base) scope
// that holds memories. Nothing runs when scopes cannot be enumerated.
func (h *HealthChecker) runChecks(ctx context.Context) {
	scopes, err := listScopes(ctx, h.repo)
	if err != nil {
		logger.Errorf(ctx, "health-checker: list scopes failed: %v", err)
		return
	}
	logger.Infof(ctx, "health-checker: starting daily health check over %d scope(s)", len(scopes))

	var issues []*types.MemoryHealthIssue
	for _, sc := range scopes {
		if ctx.Err() != nil {
			return
		}
		if sc.TenantID == "" {
			continue
		}
		report, err := h.runAllChecks(ctx, sc.TenantID, sc.KbID, 0)
		if err != nil {
			logger.Errorf(ctx, "health-checker: assessment failed: %v", err)
			continue
		}
		issues = append(issues, report.Issues...)
	}

	total := len(issues)
	bySeverity := make(map[string]int)
	criticalCount := 0
	for _, issue := range issues {
		bySeverity[issue.Severity]++
		if issue.Severity == "critical" {
			criticalCount++
			logger.Warnf(ctx, "health-checker: CRITICAL issue: %s - %s", issue.Type, issue.Description)
		}
	}

	logger.Infof(ctx, "health-checker: complete - %d total issues (%d critical)", total, criticalCount)
	if criticalCount > 0 {
		logger.Warnf(ctx, "health-checker: %d critical issues detected", criticalCount)
	}
}

// checkOrphans detects memories with zero tags and zero relations.
func (h *HealthChecker) checkOrphans(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	var issues []*types.MemoryHealthIssue
	for _, mem := range memories {
		if len(mem.Tags) == 0 && mem.HubScore == 0 {
			issues = append(issues, &types.MemoryHealthIssue{
				Type:        "orphan",
				MemoryID:    mem.ID,
				Description: "Memory has no tags and no relations (hub_score=0)",
				Severity:    "medium",
				Suggestion:  "Add relevant tags or create relations to integrate this memory into the knowledge graph",
			})
		}
	}
	return issues
}

// checkStaleFacts detects memories older than 180 days with low importance
// and no recent access.
func (h *HealthChecker) checkStaleFacts(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	now := time.Now()
	var issues []*types.MemoryHealthIssue
	for _, mem := range memories {
		age := now.Sub(mem.CreatedAt)
		recentlyAccessed := mem.LastAccessedAt != nil && now.Sub(*mem.LastAccessedAt) <= 90*24*time.Hour
		if age.Hours() > 180*24 && mem.Importance < 1 && !recentlyAccessed {
			issues = append(issues, &types.MemoryHealthIssue{
				Type:        "stale",
				MemoryID:    mem.ID,
				Description: fmt.Sprintf("Memory is %.0f days old with low importance (%d)", age.Hours()/24, mem.Importance),
				Severity:    "low",
				Suggestion:  "Review and either boost importance or allow automatic pruning",
			})
		}
	}
	return issues
}

// checkDuplications compares complete normalized content within one KB. A
// shared prefix alone does not establish duplication.
func (h *HealthChecker) checkDuplications(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	var issues []*types.MemoryHealthIssue
	type key struct{ tenant, kb, content string }
	seen := make(map[key]string)
	for _, mem := range memories {
		content := strings.Join(strings.Fields(normalizedAssertionText(mem.Content)), " ")
		if content == "" {
			continue
		}
		k := key{mem.TenantID, mem.KbID, content}
		if existingID, ok := seen[k]; ok && existingID != mem.ID {
			issues = append(issues, &types.MemoryHealthIssue{
				Type: "duplication", MemoryID: mem.ID,
				Description: fmt.Sprintf("Possible duplicate of memory %s (same normalized content)", existingID),
				Severity:    "medium", Suggestion: "Review and merge if truly duplicate",
			})
		} else {
			seen[k] = mem.ID
		}
	}
	return issues
}

// checkGraphFragmentation detects memories with hub_score=0 that are isolated.
func (h *HealthChecker) checkGraphFragmentation(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	var issues []*types.MemoryHealthIssue
	var isolatedCount, totalCount int

	for _, mem := range memories {
		totalCount++
		if mem.HubScore == 0 && math.Abs(float64(mem.Importance)) < 2 {
			isolatedCount++
		}
	}

	if totalCount > 0 {
		ratio := float64(isolatedCount) / float64(totalCount) * 100
		if ratio > 30 {
			issues = append(issues, &types.MemoryHealthIssue{
				Type:        "graph_fragmentation",
				MemoryID:    "",
				Description: fmt.Sprintf("%.0f%% of memories (%.0f/%.0f) are isolated nodes in the graph", ratio, float64(isolatedCount), float64(totalCount)),
				Severity:    "high",
				Suggestion:  "Run the auto-linker or consolidator to connect isolated memories",
			})
		}
	}

	return issues
}

// checkVerdictConsistency detects WIP memories unchanged for >30 days.
func (h *HealthChecker) checkVerdictConsistency(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	now := time.Now()
	var issues []*types.MemoryHealthIssue
	for _, mem := range memories {
		if mem.Verdict == types.VerdictWIP {
			daysSinceUpdate := int(now.Sub(mem.UpdatedAt).Hours() / 24)
			if daysSinceUpdate > 30 {
				issues = append(issues, &types.MemoryHealthIssue{
					Type:        "verdict_consistency",
					MemoryID:    mem.ID,
					Description: fmt.Sprintf("WIP memory unchanged for %d days", daysSinceUpdate),
					Severity:    "warning",
					Suggestion:  "Review and update verdict or remove the WIP status",
				})
			}
		}
	}
	return issues
}

// checkEmbeddingDimension flags embedding configurations incompatible with
// the stored rows. Rows written by the current schema sit zero-padded at
// exactly types.MemoryEmbeddingDim, so any embedder with dims up to that
// width is compatible; only larger embedders are flagged. Legacy rows stored
// at the embedder's native width keep the plain stored-vs-embedder comparison.
func (h *HealthChecker) checkEmbeddingDimension(storedDim, embedderDim int) []*types.MemoryHealthIssue {
	if storedDim == 0 || embedderDim == 0 {
		return nil
	}
	if storedDim == types.MemoryEmbeddingDim {
		if embedderDim > types.MemoryEmbeddingDim {
			return []*types.MemoryHealthIssue{{
				Type:        "embedding_dimension_mismatch",
				Severity:    "critical",
				Description: fmt.Sprintf("Embedder produces %d dimensions but the memory embedding column only supports up to %d", embedderDim, types.MemoryEmbeddingDim),
				Suggestion:  fmt.Sprintf("Switch to an embedding model with at most %d dimensions", types.MemoryEmbeddingDim),
			}}
		}
		return nil
	}
	// Legacy rows stored at the embedder's own dimensionality: only a real
	// model switch is a mismatch.
	if storedDim != embedderDim {
		return []*types.MemoryHealthIssue{{
			Type:        "embedding_dimension_mismatch",
			Severity:    "critical",
			Description: fmt.Sprintf("Stored embeddings have %d dimensions but embedder produces %d dimensions", storedDim, embedderDim),
			Suggestion:  "Re-ingest all memories with the current embedding model or switch to a compatible model",
		}}
	}
	return nil
}
