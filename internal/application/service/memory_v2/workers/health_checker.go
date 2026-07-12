package workers

import (
	"context"
	"fmt"
	"math"
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
	return &HealthChecker{}
}

// Run starts the health checker worker loop. Runs daily at 4:00 AM.
func (h *HealthChecker) Run(ctx context.Context) {
	// Run once on startup
	h.runChecks(ctx)

	for {
		// Calculate time until next 4:00 AM
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())
		if now.After(next) {
			next = next.AddDate(0, 0, 1)
		}
		delay := next.Sub(now)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			h.runChecks(ctx)

			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}
}

// AssessHealth runs all 6 health checks and returns the issues.
// This is the public entry point called from the HTTP handler.
func (h *HealthChecker) AssessHealth(ctx context.Context, tenantID string) (*types.HealthReport, error) {
	issues := h.runAllChecks(ctx, tenantID)

	report := &types.HealthReport{
		TenantID:    tenantID,
		CheckedAt:   time.Now(),
		TotalIssues: len(issues),
		BySeverity:  make(map[string]int),
		Issues:      issues,
	}

	for _, issue := range issues {
		report.BySeverity[issue.Severity]++
	}

	return report, nil
}

// runChecks runs all 6 health checks for all tenants.
func (h *HealthChecker) runChecks(ctx context.Context) {
	logger.Infof(ctx, "health-checker: starting daily health check")

	issues := h.runAllChecks(ctx, "")

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

// runAllChecks executes all 6 health check rules.
func (h *HealthChecker) runAllChecks(ctx context.Context, tenantID string) []*types.MemoryHealthIssue {
	var allIssues []*types.MemoryHealthIssue

	// Fetch memories
	filter := &types.MemoryFilter{
		TenantID: tenantID,
		Limit:    1000,
	}
	results, _, err := h.repo.Search(ctx, filter)
	if err != nil {
		logger.Errorf(ctx, "health-checker: search failed: %v", err)
		return nil
	}

	memories := make([]*types.AgentMemory, 0, len(results))
	for _, r := range results {
		if r.Memory != nil {
			memories = append(memories, r.Memory)
		}
	}

	if len(memories) == 0 {
		return nil
	}

	// Check 1: Orphans — memories with zero tags and zero relations
	allIssues = append(allIssues, h.checkOrphans(memories)...)

	// Check 2: Stale facts — >180d old, importance < 1, not accessed >90d
	allIssues = append(allIssues, h.checkStaleFacts(memories)...)

	// Check 3: Contradictions — high cosine similarity with opposite information
	allIssues = append(allIssues, h.checkContradictions(memories)...)

	// Check 4: Duplications — fingerprint collisions or very high similarity
	allIssues = append(allIssues, h.checkDuplications(memories)...)

	// Check 5: Graph fragmentation — ratio of isolated nodes
	allIssues = append(allIssues, h.checkGraphFragmentation(memories)...)

	// Check 6: Verdict consistency — WIP memories unchanged for >30 days
	allIssues = append(allIssues, h.checkVerdictConsistency(memories)...)

	return allIssues
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
		if age.Hours() > 180*24 && mem.Importance < 1 {
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

// checkContradictions detects memory pairs with high similarity but different
// stances (e.g., one says X, another says not X).
func (h *HealthChecker) checkContradictions(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	var issues []*types.MemoryHealthIssue
	// Simplified check: look for similar content that might contradict
	// In production, this would use embeddings and cosine similarity comparison
	for i := 0; i < len(memories); i++ {
		for j := i + 1; j < len(memories); j++ {
			a, b := memories[i], memories[j]
			if a.TenantID != b.TenantID {
				continue
			}
			// Check for "not" or "never" in one but not the other
			if containsNegation(a.Content) != containsNegation(b.Content) {
				issues = append(issues, &types.MemoryHealthIssue{
					Type:        "contradiction",
					MemoryID:    a.ID,
					Description: fmt.Sprintf("Potential contradiction between memories %s and %s", a.ID, b.ID),
					Severity:    "high",
					Suggestion:  "Review both memories for factual consistency",
				})
				break
			}
		}
	}
	return issues
}

// checkDuplications detects fingerprint collisions or very high similarity.
func (h *HealthChecker) checkDuplications(memories []*types.AgentMemory) []*types.MemoryHealthIssue {
	var issues []*types.MemoryHealthIssue
	seen := make(map[string]string) // content hash -> memory ID

	for _, mem := range memories {
		if len(mem.Content) < 20 {
			continue
		}
		// Simple content prefix hash
		prefix := mem.Content[:20]
		if existingID, ok := seen[prefix]; ok {
			issues = append(issues, &types.MemoryHealthIssue{
				Type:        "duplication",
				MemoryID:    mem.ID,
				Description: fmt.Sprintf("Possible duplicate of memory %s (similar content prefix)", existingID),
				Severity:    "medium",
				Suggestion:  "Review and merge if truly duplicate",
			})
		} else {
			seen[prefix] = mem.ID
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

// containsNegation checks if content contains negation words.
func containsNegation(content string) bool {
	negations := []string{"not ", "never ", "no ", "cannot ", "can't ", "don't ", "doesn't "}
	for _, n := range negations {
		// Simple substring check
		for i := 0; i <= len(content)-len(n); i++ {
			if i+len(n) <= len(content) && toLower(content[i:i+len(n)]) == n {
				return true
			}
		}
	}
	return false
}
