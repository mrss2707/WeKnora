package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Auto-link calibration (bge-m3, see the memory recall eval): unrelated
// memory pairs reach at most ~0.64 cosine (p99 0.56), while restatements and
// translations of the same fact score 0.84+ and close topical neighbours
// 0.70+. 0.65 linked 0 of 768 unrelated pairs.
const (
	// DefaultLinkThreshold is the minimum cosine similarity for related_to.
	DefaultLinkThreshold = 0.65
	// linkCandidates bounds the neighbours considered per memory.
	linkCandidates = 10
)

// AutoLinker links a memory to its nearest neighbours in the same knowledge
// base with "related_to" edges (weight = cosine similarity), and adds a
// "justifies" edge from a decision to a memory it covers. Neighbours come from
// the vector index using the memory's stored embedding, so linking costs one
// indexed query and never calls the embedding model.
type AutoLinker struct {
	repo      interfaces.MemoryRepositoryV2
	embedder  embedding.Embedder
	resolver  ModelResolver
	threshold float64
}

// NewAutoLinker creates a new AutoLinker. embedder is only used for memories
// that arrive without a stored vector.
func NewAutoLinker(repo interfaces.MemoryRepositoryV2, embedder embedding.Embedder) *AutoLinker {
	return &AutoLinker{repo: repo, embedder: embedder, threshold: DefaultLinkThreshold}
}

// WithResolver makes the linker resolve each memory's embedding model.
func (a *AutoLinker) WithResolver(r ModelResolver) *AutoLinker {
	a.resolver = r
	return a
}

// WithThreshold overrides the related_to similarity threshold.
func (a *AutoLinker) WithThreshold(t float64) *AutoLinker {
	if t > 0 {
		a.threshold = t
	}
	return a
}

// LinkMemory links a saved memory to its neighbours. It returns the number of
// relations created. Errors are logged; linking never fails a save.
func (a *AutoLinker) LinkMemory(ctx context.Context, memory *types.AgentMemory) int {
	if memory == nil {
		return 0
	}
	created, err := a.LinkMemoryWithError(ctx, memory)
	if err != nil {
		logger.Warnf(ctx, "auto-linker: memory %s: %v", memory.ID, err)
	}
	return created
}

// LinkMemoryWithError lets the durable queue retry partial failures. Inserting
// an already-existing edge is success, not a new relation or an error.
func (a *AutoLinker) LinkMemoryWithError(ctx context.Context, memory *types.AgentMemory) (created int, resultErr error) {
	if memory == nil || memory.ID == "" || memory.TenantID == "" {
		return 0, fmt.Errorf("invalid graph memory scope")
	}
	vector := memory.Embedding.Slice()
	if isZeroVector(vector) {
		vector = a.embed(ctx, memory)
		if isZeroVector(vector) {
			return 0, fmt.Errorf("graph memory has no usable embedding")
		}
	}
	filter := &types.MemoryFilter{TenantID: memory.TenantID, KbID: memory.KbID, EmbeddingModelID: metadataEmbeddingModel(memory.Metadata)}
	neighbours, err := a.repo.CosineSearch(ctx, filter, vector, linkCandidates+1)
	if err != nil {
		return 0, fmt.Errorf("neighbour search failed: %w", err)
	}
	touched := []string{memory.ID}
	defer func() {
		if len(touched) < 2 {
			return
		}
		if hr, ok := a.repo.(interfaces.MemoryV2HubRefresher); ok {
			if err := hr.RefreshHubScores(ctx, memory.TenantID, touched); err != nil && resultErr == nil {
				resultErr = err
			}
		}
	}()
	link := func(from, to *types.AgentMemory, kind string, weight float64) error {
		added, err := a.relateVersioned(ctx, from, to, kind, weight)
		if added {
			created++
		}
		return err
	}
	for _, n := range neighbours {
		candidate := n.Memory
		if candidate == nil || candidate.ID == memory.ID || n.Score < a.threshold {
			continue
		}
		if candidate.TenantID != memory.TenantID || candidate.KbID != memory.KbID {
			continue
		}
		touched = append(touched, candidate.ID)
		if err := link(memory, candidate, "related_to", n.Score); err != nil {
			return created, err
		}
		switch {
		case candidate.Verdict == types.VerdictDecision && memory.Verdict != types.VerdictDecision:
			if err := link(candidate, memory, "justifies", n.Score); err != nil {
				return created, err
			}
		case memory.Verdict == types.VerdictDecision && candidate.Verdict != types.VerdictDecision:
			if err := link(memory, candidate, "justifies", n.Score); err != nil {
				return created, err
			}
		}
	}
	return created, nil
}

func (a *AutoLinker) relateVersioned(ctx context.Context, from, to *types.AgentMemory, kind string, weight float64) (bool, error) {
	if kind == "related_to" && to.ID < from.ID {
		from, to = to, from
	}
	relation := &types.MemoryRelation{TenantID: from.TenantID, FromUUID: from.ID, ToUUID: to.ID, RelationType: kind, Weight: weight, CreatedAt: time.Now()}
	if writer, ok := a.repo.(interfaces.MemoryV2VersionedRelationWriter); ok {
		return writer.CreateRelationForVersions(ctx, relation, from.UpdatedAt, to.UpdatedAt)
	}
	if err := a.repo.CreateRelation(ctx, relation); err != nil {
		return false, err
	}
	return true, nil
}

// embed computes a vector for a memory that has none stored.
func (a *AutoLinker) embed(ctx context.Context, memory *types.AgentMemory) []float32 {
	emb := a.embedder
	if a.resolver != nil {
		e, _, err := a.resolver.Embedder(ctx, memory.TenantID, memory.KbID)
		if err != nil {
			logger.Errorf(ctx, "auto-linker: resolve embedder: %v", err)
			return nil
		}
		emb = e
	}
	if emb == nil {
		return nil
	}
	v, err := emb.Embed(ctx, memory.Content)
	if err != nil {
		logger.Errorf(ctx, "auto-linker: embedding failed: %v", err)
		return nil
	}
	return v
}

func isZeroVector(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// metadataEmbeddingModel reads metadata.embedding_model_id.
func metadataEmbeddingModel(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m struct {
		ID string `json:"embedding_model_id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	return m.ID
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
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
