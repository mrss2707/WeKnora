package memory_v2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
)

// ErrMemoryValidation is returned when memory content fails validation.
type ErrMemoryValidation struct {
	Message string
}

func (e *ErrMemoryValidation) Error() string {
	return "memory validation error: " + e.Message
}

// ErrMemoryDuplicate is returned when memory is a near-exact semantic duplicate.
type ErrMemoryDuplicate struct {
	ExistingID string
	Similarity float64
}

func (e *ErrMemoryDuplicate) Error() string {
	return fmt.Sprintf("memory duplicate: %.2f similarity with %s", e.Similarity, e.ExistingID)
}

// SaveMemory runs the full ingestion pipeline and returns the result.
func (s *MemoryServiceV2Impl) SaveMemory(ctx context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error) {
	// Step 0: Ensure background workers are initialized
	s.ensureWorkers(ctx)

	if memory == nil {
		return nil, fmt.Errorf("memory_v2: SaveMemory called with nil memory")
	}

	// Step 1: Validate
	if err := validateContent(memory.Content); err != nil {
		return nil, err
	}

	// Step 2: SHA256 fingerprint dedup
	fingerprint := computeFingerprint(memory.Content)
	fingerprintStr := fingerprint // capture for use in memory struct
	memory.Fingerprint = &fingerprintStr
	existing, err := s.findByFingerprint(ctx, memory.TenantID, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("fingerprint lookup failed: %w", err)
	}
	if existing != nil {
		// Exact duplicate found — no error, just report it
		return &types.SaveMemoryResult{
			Memory:  existing,
			Created: false,
		}, nil
	}

	// Step 3: Semantic dedup (requires embedding)
	embedder, err := s.getEmbedder(ctx)
	if err != nil {
		return nil, fmt.Errorf("embedder not available: %w", err)
	}
	embedding, err := embedder.Embed(ctx, memory.Content)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	memory.Embedding = pgvector.NewVector(embedding)

	semResult, err := s.checkSemanticDedup(ctx, memory, embedding)
	if err != nil {
		return nil, err
	}
	if semResult != nil {
		return semResult, nil
	}

	// Step 4: Type detection
	memory.MemoryType = detectMemoryType(memory.Content)

	// Step 5: Tag suggestions
	memory.Tags = suggestTags(memory.Content, memory.MemoryType)

	// Step 6: Quality scoring / importance
	memory.Importance = computeImportance(memory.Content, memory.MemoryType)

	// Step 7: Tier assignment
	memory.Tier = assignTier(memory.Importance, memory.MemoryType)

	// Set defaults
	if memory.Verdict == "" {
		memory.Verdict = types.VerdictNone
	}
	if memory.ID == "" {
		memory.ID = ""
	}
	now := time.Now()
	memory.CreatedAt = now
	memory.UpdatedAt = now

	// Step 8: Store (already has embedding)
	if err := s.repo.Create(ctx, memory); err != nil {
		return nil, fmt.Errorf("store failed: %w", err)
	}

	// Step 9: Lint on write (pass pre-computed embedding to avoid re-embed)
	lintIssues := RunLintOnWrite(ctx, memory, s.repo, s.config.LintOnWrite, embedding)

	// Step 10: Enqueue for batch entity extraction (skip if workers not initialized)
	if s.entityExtractor != nil {
		s.entityExtractor.Enqueue(memory)
	}

	// Step 10b: Auto-link to semantically similar memories
	if s.autoLinker != nil {
		s.autoLinker.LinkMemory(ctx, memory)
	}

	result := &types.SaveMemoryResult{
		Memory:     memory,
		Created:    true,
		LintIssues: lintIssues,
	}

	return result, nil
}

// validateContent validates memory content.
func validateContent(content string) error {
	if len(content) < 10 {
		return &ErrMemoryValidation{
			Message: fmt.Sprintf("content length %d is less than minimum 10 characters", len(content)),
		}
	}
	if len(content) > 10000 {
		return &ErrMemoryValidation{
			Message: fmt.Sprintf("content length %d exceeds maximum 10000 characters", len(content)),
		}
	}

	// Count non-whitespace characters
	nonWhitespace := 0
	for _, r := range content {
		if !unicode.IsSpace(r) {
			nonWhitespace++
		}
	}
	if nonWhitespace < 5 {
		return &ErrMemoryValidation{
			Message: fmt.Sprintf("content has only %d non-whitespace characters, minimum is 5", nonWhitespace),
		}
	}
	return nil
}

// computeFingerprint returns a SHA256 hex fingerprint of the content.
func computeFingerprint(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// findByFingerprint looks up a memory by its content fingerprint.
func (s *MemoryServiceV2Impl) findByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return s.repo.GetByFingerprint(ctx, tenantID, fingerprint)
}

// checkSemanticDedup checks cosine similarity against existing memories.
// Returns:
//   - nil, nil: unique memory, proceed with save
//   - *SaveMemoryResult, nil: duplicate or merged
//   - nil, error: system error
func (s *MemoryServiceV2Impl) checkSemanticDedup(ctx context.Context, memory *types.AgentMemory, vector []float32) (*types.SaveMemoryResult, error) {
	if len(vector) == 0 {
		return nil, nil
	}

	// Search for semantically similar memories
	filter := &types.MemoryFilter{
		TenantID: memory.TenantID,
		KbID:     memory.KbID,
	}
	filter.Limit = s.config.SemanticDedup.MaxMerges
	if filter.Limit <= 0 {
		filter.Limit = 3
	}

	similar, err := s.repo.CosineSearch(ctx, filter, vector, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("semantic dedup search failed: %w", err)
	}

	for _, sr := range similar {
		if sr.Memory == nil {
			continue
		}
		score := sr.Score

		switch {
		case score > s.config.SemanticDedup.ExactThreshold:
			// Near-exact duplicate: reject
			return nil, &ErrMemoryDuplicate{
				ExistingID: sr.Memory.ID,
				Similarity: score,
			}

		case score > s.config.SemanticDedup.NearThreshold:
			// Near-duplicate: merge content
			return s.mergeMemory(ctx, memory, sr.Memory, score)
		}
	}

	return nil, nil
}

// mergeMemory merges new content into an existing near-duplicate memory.
func (s *MemoryServiceV2Impl) mergeMemory(ctx context.Context, newMem, existingMem *types.AgentMemory, similarity float64) (*types.SaveMemoryResult, error) {
	maxChars := s.config.SemanticDedup.MergeMaxChars
	if maxChars <= 0 {
		maxChars = 2000
	}

	// Merge content: combine with separator, respecting maxChars
	mergedContent := existingMem.Content + "\n---\n" + newMem.Content
	if len(mergedContent) > maxChars {
		mergedContent = mergedContent[:maxChars]
	}

	existingMem.Content = mergedContent
	existingMem.UpdatedAt = time.Now()

	// Update importance to the max of both
	if newMem.Importance > existingMem.Importance {
		existingMem.Importance = newMem.Importance
	}

	if err := s.repo.Update(ctx, existingMem); err != nil {
		return nil, fmt.Errorf("merge update failed: %w", err)
	}

	return &types.SaveMemoryResult{
		Memory:  existingMem,
		Created: false,
	}, nil
}

// detectMemoryType uses keyword heuristics to detect the memory type.
func detectMemoryType(content string) string {
	lower := strings.ToLower(content)

	episodicKeywords := []string{"happened", "occurred", "was", "were", "today", "yesterday", "earlier", "remember", "recall", "event"}
	semanticKeywords := []string{"is a", "is an", "are", "means", "defined as", "refers to", "consists of", "is used", "typically"}
	proceduralKeywords := []string{"steps", "how to", "procedure", "process", "workflow", "first", "then", "next", "finally", "instructions"}
	decisionKeywords := []string{"decided", "chose", "selected", "approved", "agreed", "resolution", "conclusion", "decision"}
	preferenceKeywords := []string{"prefer", "like", "dislike", "favorite", "better", "rather", "would rather", "opinion"}

	score := map[string]int{
		"episodic":    0,
		"semantic":    0,
		"procedural":  0,
		"decision":    0,
		"preference":  0,
	}

	for _, kw := range episodicKeywords {
		if strings.Contains(lower, kw) {
			score["episodic"]++
		}
	}
	for _, kw := range semanticKeywords {
		if strings.Contains(lower, kw) {
			score["semantic"]++
		}
	}
	for _, kw := range proceduralKeywords {
		if strings.Contains(lower, kw) {
			score["procedural"]++
		}
	}
	for _, kw := range decisionKeywords {
		if strings.Contains(lower, kw) {
			score["decision"]++
		}
	}
	for _, kw := range preferenceKeywords {
		if strings.Contains(lower, kw) {
			score["preference"]++
		}
	}

	bestType := "fact"
	bestScore := 0
	for typ, s := range score {
		if s > bestScore {
			bestScore = s
			bestType = typ
		}
	}

	return bestType
}

// suggestTags extracts simple keyword tags from content.
func suggestTags(content string, memoryType string) types.TagsArray {
	// Simple tag extraction: pick significant words
	words := strings.Fields(content)
	seen := make(map[string]bool)
	var tags []string

	if memoryType != "" {
		tags = append(tags, memoryType)
		seen[memoryType] = true
	}

	for _, word := range words {
		cleaned := strings.TrimFunc(strings.ToLower(word), func(r rune) bool {
			return !unicode.IsLetter(r)
		})
		if len(cleaned) < 3 || len(cleaned) > 30 {
			continue
		}
		if seen[cleaned] {
			continue
		}
		// Skip common stop words
		if isStopWord(cleaned) {
			continue
		}
		if len(tags) >= 10 {
			break
		}
		tags = append(tags, cleaned)
		seen[cleaned] = true
	}

	return tags
}

// isStopWord checks if a word is a common English stop word.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "shall": true, "can": true, "need": true, "dare": true,
		"ought": true, "used": true, "to": true, "of": true, "in": true,
		"for": true, "on": true, "with": true, "at": true, "by": true,
		"from": true, "as": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true, "between": true,
		"and": true, "but": true, "or": true, "nor": true, "not": true,
		"so": true, "yet": true, "both": true, "either": true, "neither": true,
		"this": true, "that": true, "these": true, "those": true, "i": true,
		"you": true, "he": true, "she": true, "it": true, "we": true,
		"they": true, "me": true, "him": true, "her": true, "us": true,
		"them": true, "my": true, "your": true, "his": true, "its": true,
		"our": true, "their": true, "mine": true, "yours": true, "hers": true,
		"ours": true, "theirs": true, "myself": true, "yourself": true,
		"himself": true, "herself": true, "itself": true, "ourselves": true,
		"themselves": true, "what": true, "which": true, "who": true, "whom": true,
		"whose": true, "when": true, "where": true, "why": true, "how": true,
		"all": true, "each": true, "every": true, "few": true,
		"more": true, "most": true, "other": true, "some": true, "such": true,
		"no": true, "any": true, "only": true, "own": true, "same": true,
		"than": true, "too": true, "very": true, "just": true, "about": true,
		"also": true, "if": true, "then": true, "else": true, "over": true,
	}
	return stopWords[word]
}

// computeImportance scores content from -5 to +6.
func computeImportance(content string, memoryType string) int {
	score := 0

	// Base by type
	switch memoryType {
	case "decision":
		score += 4
	case "preference":
		score += 2
	case "episodic":
		score += 1
	case "semantic":
		score += 2
	case "procedural":
		score += 3
	}

	// Content length bonus
	words := len(strings.Fields(content))
	if words > 50 {
		score += 1
	}
	if words > 100 {
		score += 1
	}

	// Keyword bonuses
	lower := strings.ToLower(content)

	// High-importance signals
	highSig := []string{"critical", "important", "urgent", "mandatory", "required", "essential", "vital", "key", "significant"}
	for _, kw := range highSig {
		if strings.Contains(lower, kw) {
			score++
			break
		}
	}

	// Decision signals
	decisionSig := []string{"decided", "approved", "agreed", "confirmed", "finalized", "resolved"}
	for _, kw := range decisionSig {
		if strings.Contains(lower, kw) {
			score += 2
			break
		}
	}

	// Low-importance signals
	lowSig := []string{"maybe", "perhaps", "possibly", "might", "could be", "not sure", "unclear", "ambiguous"}
	for _, kw := range lowSig {
		if strings.Contains(lower, kw) {
			score--
			break
		}
	}

	// Clamp to [-5, +6]
	return clampInt(score, -5, 6)
}

// assignTier determines the storage tier (0-3) from importance and type.
func assignTier(importance int, memoryType string) int {
	switch {
	case memoryType == "decision":
		return 1 // Core
	case importance >= 4:
		return 1 // Core
	case importance >= 2:
		return 2 // Standard
	case importance <= -3:
		return 3 // Edge
	default:
		return 2 // Standard
	}
}

// clampInt clamps a value to [min, max].
func clampInt(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

// cosineSimilarity computes cosine similarity between two vectors.
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