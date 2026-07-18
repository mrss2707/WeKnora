package workers

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AutoLinker creates relations between memories based on tag overlap and semantic similarity.
type AutoLinker struct {
	repo     interfaces.MemoryRepositoryV2
	embedder embedding.Embedder
}

// NewAutoLinker creates a new AutoLinker.
func NewAutoLinker(repo interfaces.MemoryRepositoryV2, embedder embedding.Embedder) *AutoLinker {
	return &AutoLinker{
		repo:     repo,
		embedder: embedder,
	}
}

// LinkMemory checks a newly saved memory against existing memories and creates
// relations where appropriate. Called per-memory after save.
func (a *AutoLinker) LinkMemory(ctx context.Context, memory *types.AgentMemory) {
	if memory == nil || memory.ID == "" {
		return
	}

	// Find memories with overlapping tags (2+ shared tags)
	tagCandidates, err := a.findTagOverlapMemories(ctx, memory)
	if err != nil {
		logger.Errorf(ctx, "auto-linker: tag overlap search failed: %v", err)
		return
	}

	// Embed the new memory for cosine comparison
	vector, err := a.embedder.Embed(ctx, memory.Content)
	if err != nil {
		logger.Errorf(ctx, "auto-linker: embedding failed: %v", err)
		return
	}

	for _, candidate := range tagCandidates {
		if candidate.ID == memory.ID {
			continue
		}

		// Get embedding for candidate (embed on demand)
		candidateVec, err := a.embedder.Embed(ctx, candidate.Content)
		if err != nil {
			continue
		}

		similarity := cosineSimilarity(vector, candidateVec)

		if similarity > 0.65 {
			// Create related_to relation
			relation := &types.MemoryRelation{
				TenantID:  memory.TenantID,
				FromUUID:  memory.ID,
				ToUUID:    candidate.ID,
				RelationType: "related_to",
				Weight:    similarity,
				CreatedAt: time.Now(),
			}
			// Store using repo
				if err := a.repo.CreateRelation(ctx, relation); err != nil {
					logger.Errorf(ctx, "auto-linker: failed to create relation: %v", err)
				}

			// If the candidate has a decision verdict, it justifies this memory
			if candidate.Verdict == types.VerdictDecision {
				justifiesRel := &types.MemoryRelation{
					TenantID:  memory.TenantID,
					FromUUID:  candidate.ID,
					ToUUID:    memory.ID,
					RelationType: "justifies",
					Weight:    similarity,
					CreatedAt: time.Now(),
				}
				if err := a.repo.CreateRelation(ctx, justifiesRel); err != nil {
					logger.Errorf(ctx, "auto-linker: failed to create justifies relation: %v", err)
				}
			}
		}
	}
}

// findTagOverlapMemories finds memories from the same tenant that share at
// least 2 tags with the given memory.
func (a *AutoLinker) findTagOverlapMemories(ctx context.Context, memory *types.AgentMemory) ([]*types.AgentMemory, error) {
	if len(memory.Tags) < 2 {
		return nil, nil
	}

	// Search for memories with similar tag sets
	filter := &types.MemoryFilter{
		TenantID: memory.TenantID,
		Limit:    50,
	}

	// Use repo search as a starting point
	results, _, err := a.repo.Search(ctx, filter)
	if err != nil {
		return nil, err
	}

	var candidates []*types.AgentMemory
	for _, r := range results {
		if r.Memory == nil || r.Memory.ID == memory.ID {
			continue
		}
		shared := countSharedTags(memory.Tags, r.Memory.Tags)
		if shared >= 2 {
			candidates = append(candidates, r.Memory)
		}
	}

	return candidates, nil
}

// countSharedTags counts the number of shared tags between two slices.
func countSharedTags(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, t := range a {
		set[t] = true
	}
	count := 0
	for _, t := range b {
		if set[t] {
			count++
		}
	}
	return count
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Simple Newton's method for sqrt
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
