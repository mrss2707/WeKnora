package workers

import (
	"context"
	"encoding/json"
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
	if memory == nil || memory.ID == "" || memory.TenantID == "" {
		return 0
	}
	vector := memory.Embedding.Slice()
	if isZeroVector(vector) {
		vector = a.embed(ctx, memory)
		if vector == nil {
			return 0
		}
	}

	filter := &types.MemoryFilter{
		TenantID:         memory.TenantID,
		KbID:             memory.KbID,
		EmbeddingModelID: metadataEmbeddingModel(memory.Metadata),
	}
	neighbours, err := a.repo.CosineSearch(ctx, filter, vector, linkCandidates+1)
	if err != nil {
		logger.Errorf(ctx, "auto-linker: neighbour search failed for %s: %v", memory.ID, err)
		return 0
	}

	created := 0
	touched := []string{memory.ID}
	defer func() {
		if created == 0 {
			return
		}
		if hr, ok := a.repo.(interfaces.MemoryV2HubRefresher); ok {
			if err := hr.RefreshHubScores(ctx, memory.TenantID, touched); err != nil {
				logger.Warnf(ctx, "auto-linker: hub score refresh failed: %v", err)
			}
		}
	}()
	for _, n := range neighbours {
		c := n.Memory
		if c == nil || c.ID == memory.ID || n.Score < a.threshold {
			continue
		}
		// Defence in depth: the search is scoped, but never link across
		// tenants or knowledge bases.
		if c.TenantID != memory.TenantID || c.KbID != memory.KbID {
			continue
		}
		if a.relate(ctx, memory.TenantID, memory.ID, c.ID, "related_to", n.Score) {
			created++
			touched = append(touched, c.ID)
		}
		switch {
		case c.Verdict == types.VerdictDecision && memory.Verdict != types.VerdictDecision:
			if a.relate(ctx, memory.TenantID, c.ID, memory.ID, "justifies", n.Score) {
				created++
			}
		case memory.Verdict == types.VerdictDecision && c.Verdict != types.VerdictDecision:
			if a.relate(ctx, memory.TenantID, memory.ID, c.ID, "justifies", n.Score) {
				created++
			}
		}
	}
	return created
}

func (a *AutoLinker) relate(ctx context.Context, tenantID, from, to, kind string, weight float64) bool {
	// related_to is symmetric: store it once, in canonical order, so A~B and
	// B~A never become two edges (which would also double hub scores).
	if kind == "related_to" && to < from {
		from, to = to, from
	}
	err := a.repo.CreateRelation(ctx, &types.MemoryRelation{
		TenantID: tenantID, FromUUID: from, ToUUID: to,
		RelationType: kind, Weight: weight, CreatedAt: time.Now(),
	})
	if err != nil {
		logger.Errorf(ctx, "auto-linker: create %s relation %s->%s: %v", kind, from, to, err)
		return false
	}
	return true
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
