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
// Lint is advisory and does not block the save.
func RunLintOnWrite(ctx context.Context, memory *types.AgentMemory, repo interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig) []types.MemoryLintIssue {
	if !config.Enabled {
		return nil
	}

	var issues []types.MemoryLintIssue

	// Rule 1: Orphans check — memory with no tags and no relations
	issues = append(issues, lintOrphans(ctx, memory, repo)...)

	// Rule 2: Staleness — memory created long ago with low importance
	issues = append(issues, lintStaleness(ctx, memory, config)...)

	// Rule 3: Contradiction — high similarity with memories of opposite verdict
	issues = append(issues, lintContradiction(ctx, memory, repo, config)...)

	// Rule 4: Duplication — potential fingerprint collision with existing memories
	issues = append(issues, lintDuplication(ctx, memory, repo, config)...)

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
func lintContradiction(_ context.Context, _ *types.AgentMemory, _ interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig) []types.MemoryLintIssue {
	_ = config.ContradictionThreshold
	// This check requires full scan of similar memories, which is expensive.
	// It will be implemented more thoroughly in the health checker.
	return nil
}

// lintDuplication checks for potential duplicate fingerprints.
func lintDuplication(_ context.Context, _ *types.AgentMemory, _ interfaces.MemoryRepositoryV2, config types.LintOnWriteConfig) []types.MemoryLintIssue {
	_ = config.NearDuplicateThreshold
	// This check requires comparing against all existing memories.
	// The dedup stage already handles this; lint is advisory.
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
