package memory_v2

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// RunLintOnWrite runs all 6 lint rules on a newly saved memory.
// embedding is the pre-computed vector from SaveMemory to avoid duplicate embed cost.
// Lint is advisory and does not block the save.
func RunLintOnWrite(ctx context.Context, memory *types.AgentMemory, repo interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig, embedding []float32) []types.MemoryLintIssue {
	if !config.Enabled {
		return nil
	}

	var issues []types.MemoryLintIssue

	// Rule 1: Orphans check — memory with no tags and no relations
	issues = append(issues, lintOrphans(ctx, memory, repo)...)

	// Rule 2: Staleness — memory created long ago with low importance
	issues = append(issues, lintStaleness(ctx, memory, config)...)

	// Rule 3: Contradiction — high similarity with memories of opposite verdict
	issues = append(issues, lintContradiction(ctx, memory, repo, config, embedding)...)

	// Rule 4: Duplication — potential fingerprint collision with existing memories
	issues = append(issues, lintDuplication(ctx, memory, repo, config, embedding)...)

	// Rule 5: Graph fragmentation — memory has no relations and low hub potential
	issues = append(issues, lintGraphFragmentation(ctx, memory)...)

	// Rule 6: Verdict consistency — WIP that hasn't been updated
	issues = append(issues, lintVerdictConsistency(ctx, memory)...)

	if len(issues) > 0 {
		logger.Infof(ctx, "lint: memory %s produced %d issues", memory.ID, len(issues))
	}

	return issues
}

// lintOrphans checks if a memory has zero tags and zero relations.
func lintOrphans(_ context.Context, memory *types.AgentMemory, _ interfaces.MemoryRepositoryV2) []types.MemoryLintIssue {
	if len(memory.Tags) == 0 && memory.HubScore == 0 {
		return []types.MemoryLintIssue{{
			Rule:     "orphans",
			Severity: "warning",
			Message:  fmt.Sprintf("Memory %s has no tags and no relations (hub_score=0)", memory.ID),
			SourceID: memory.ID,
		}}
	}
	return nil
}

// lintStaleness checks if the memory is old with low importance.
func lintStaleness(_ context.Context, memory *types.AgentMemory, config types.LintOnWriteConfig) []types.MemoryLintIssue {
	staleThreshold := config.StaleThresholdDays
	if staleThreshold <= 0 {
		staleThreshold = 90
	}

	age := time.Since(memory.CreatedAt)
	if age.Hours() > float64(staleThreshold*24) && memory.Importance < 1 {
		return []types.MemoryLintIssue{{
			Rule:     "staleness",
			Severity: "info",
			Message:  fmt.Sprintf("Memory %s is %d days old with low importance (%d)", memory.ID, int(age.Hours()/24), memory.Importance),
			SourceID: memory.ID,
		}}
	}
	return nil
}

// lintContradiction checks for near-duplicate memories with opposite verdicts.
func lintContradiction(ctx context.Context, memory *types.AgentMemory, repo interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig, embedding []float32) []types.MemoryLintIssue {
	threshold := config.ContradictionThreshold
	if threshold <= 0 {
		threshold = 0.85
	}

	if len(embedding) == 0 {
		return nil
	}

	filter := &types.MemoryFilter{
		TenantID: memory.TenantID,
		Limit:    5,
	}
	similar, err := repo.CosineSearch(ctx, filter, embedding, 5)
	if err != nil {
		logger.Warnf(ctx, "[MemoryV2] lintContradiction: CosineSearch failed for memory %s: %v", memory.ID, err)
		return nil
	}

	for _, sr := range similar {
		if sr.Memory == nil || sr.Memory.ID == memory.ID {
			continue
		}
		if sr.Score < threshold {
			continue
		}

		// Check for opposite verdicts
		oppositeVerdicts := map[types.MemoryVerdict]types.MemoryVerdict{
			types.VerdictFixed:   types.VerdictRefuted,
			types.VerdictRefuted: types.VerdictFixed,
			types.VerdictWIP:     types.VerdictDecision,
		}
		if opposite, ok := oppositeVerdicts[memory.Verdict]; ok && sr.Memory.Verdict == opposite {
			return []types.MemoryLintIssue{{
				Rule:     "contradiction",
				Severity: "critical",
				Message:  fmt.Sprintf("Memory %s contradicts %s (verdict %s vs %s, similarity %.2f)", memory.ID, sr.Memory.ID, memory.Verdict, sr.Memory.Verdict, sr.Score),
				SourceID: sr.Memory.ID,
			}}
		}
	}
	return nil
}

// lintDuplication checks for near-duplicate memories by cosine similarity.
func lintDuplication(ctx context.Context, memory *types.AgentMemory, repo interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig, embedding []float32) []types.MemoryLintIssue {
	threshold := config.NearDuplicateThreshold
	if threshold <= 0 {
		threshold = 0.95
	}

	if len(embedding) == 0 {
		return nil
	}

	filter := &types.MemoryFilter{
		TenantID: memory.TenantID,
		Limit:    3,
	}
	similar, err := repo.CosineSearch(ctx, filter, embedding, 3)
	if err != nil {
		logger.Warnf(ctx, "[MemoryV2] lintDuplication: CosineSearch failed for memory %s: %v", memory.ID, err)
		return nil
	}

	for _, sr := range similar {
		if sr.Memory == nil || sr.Memory.ID == memory.ID {
			continue
		}
		if sr.Score >= threshold {
			return []types.MemoryLintIssue{{
				Rule:     "duplication",
				Severity: "warning",
				Message:  fmt.Sprintf("Memory %s is near-duplicate of %s (similarity %.2f)", memory.ID, sr.Memory.ID, sr.Score),
				SourceID: sr.Memory.ID,
			}}
		}
	}
	return nil
}

// lintGraphFragmentation checks if a memory is isolated in the graph.
func lintGraphFragmentation(_ context.Context, memory *types.AgentMemory) []types.MemoryLintIssue {
	if memory.HubScore == 0 && math.Abs(float64(memory.Importance)) < 2 {
		return []types.MemoryLintIssue{{
			Rule:     "graph_fragmentation",
			Severity: "info",
			Message:  fmt.Sprintf("Memory %s is isolated (hub_score=0, importance=%d)", memory.ID, memory.Importance),
			SourceID: memory.ID,
		}}
	}
	return nil
}

// lintVerdictConsistency checks if a WIP memory hasn't been updated recently.
func lintVerdictConsistency(_ context.Context, memory *types.AgentMemory) []types.MemoryLintIssue {
	if memory.Verdict == types.VerdictWIP {
		daysSinceUpdate := int(time.Since(memory.UpdatedAt).Hours() / 24)
		if daysSinceUpdate > 30 {
			return []types.MemoryLintIssue{{
				Rule:     "verdict_consistency",
				Severity: "warning",
				Message:  fmt.Sprintf("Memory %s is WIP but hasn't been updated in %d days", memory.ID, daysSinceUpdate),
				SourceID: memory.ID,
			}}
		}
	}
	return nil
}
