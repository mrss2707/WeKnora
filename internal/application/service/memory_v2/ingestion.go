package memory_v2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/langdata"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
)

// ErrMemoryValidation is returned when memory content fails validation.
type ErrMemoryValidation = types.ErrMemoryValidation

// SaveMemory runs the full ingestion pipeline and returns the result.
func (s *MemoryServiceV2Impl) SaveMemory(ctx context.Context, memory *types.AgentMemory) (*types.SaveMemoryResult, error) {
	// Step 0: Workers are constructed once; auto-linking needs them.
	s.initWorkers()

	if memory == nil {
		return nil, fmt.Errorf("memory_v2: SaveMemory called with nil memory")
	}

	// Step 1: Validate (pure — no repository or model touch).
	if err := validateContent(memory.Content); err != nil {
		return nil, err
	}

	// Scope guard: memories must be scoped to a tenant before any repository
	// touch. An unscoped write would create data invisible to (and hidden
	// from) every workspace filter below.
	if strings.TrimSpace(memory.TenantID) == "" {
		return nil, &ErrMemoryValidation{Message: "tenant_id is required"}
	}

	// Readiness guard: a disabled/not-ready module (config flag, Lite mode or
	// missing repository) rejects writes instead of reaching the repository.
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2 not ready: %s", reason)
	}

	// Step 2: SHA256 fingerprint dedup. The fingerprint is domain-separated by
	// tenant and knowledge base because the database uniqueness index is global.
	fingerprint := computeScopedFingerprint(memory.TenantID, memory.KbID, memory.Content)
	fingerprintStr := fingerprint // capture for use in memory struct
	memory.Fingerprint = &fingerprintStr
	existing, err := s.findByFingerprint(ctx, memory.TenantID, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("fingerprint lookup failed: %w", err)
	}
	if existing == nil {
		// Rows created before scoped fingerprints used SHA256(content). Preserve
		// exact deduplication for those rows, but only inside the requested KB.
		legacyFingerprint := computeFingerprint(memory.Content)
		legacy, legacyErr := s.findByFingerprint(ctx, memory.TenantID, legacyFingerprint)
		if legacyErr != nil {
			return nil, fmt.Errorf("legacy fingerprint lookup failed: %w", legacyErr)
		}
		if legacy != nil && legacy.KbID == memory.KbID {
			existing = legacy
		}
	}
	if existing != nil {
		// Exact duplicate found — no error, just report it
		return &types.SaveMemoryResult{
			Memory:  existing,
			Created: false,
		}, nil
	}

	// Step 3: Semantic dedup (requires embedding). The knowledge base's own
	// embedding model is used; its id is recorded on the row so vectors from
	// different models are never compared.
	embedder, modelID, err := s.embedderFor(ctx, memory.TenantID, memory.KbID)
	if err != nil {
		return nil, fmt.Errorf("embedder not available: %w", err)
	}
	embedding, err := embedder.Embed(ctx, memory.Content)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	memory.Embedding = pgvector.NewVector(embedding)
	if modelID != "" {
		memory.Metadata = setMetadataString(memory.Metadata, metadataEmbeddingModel, modelID)
	}

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
	s.recordAudit(ctx, newAudit(ctx, types.MemoryAuditCreated, nil, memory, nil))

	// Step 9: Lint on write (pass pre-computed embedding to avoid re-embed)
	lintIssues := RunLintOnWrite(ctx, memory, s.repo, s.config.LintOnWrite, embedding)

	// Step 10: Enqueue for batch entity extraction. The extractor rewrites
	// tags, so it only runs when mutating workers are armed (an unarmed
	// extractor would also buffer forever).
	if s.entityExtractor != nil && s.config.WorkerMutations {
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
	n := utf8.RuneCountInString(content)
	if n < 10 {
		return &ErrMemoryValidation{
			Message: fmt.Sprintf("content length %d is less than minimum 10 characters", n),
		}
	}
	if n > maxContentChars {
		return &ErrMemoryValidation{
			Message: fmt.Sprintf("content length %d exceeds maximum %d characters", n, maxContentChars),
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

// computeScopedFingerprint domain-separates exact deduplication by tenant and
// knowledge base while remaining compatible with the existing global index.
func computeScopedFingerprint(tenantID, kbID, content string) string {
	return computeFingerprint(tenantID + "\x00" + kbID + "\x00" + content)
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
		TenantID:         memory.TenantID,
		KbID:             memory.KbID,
		EmbeddingModelID: metadataString(memory.Metadata, metadataEmbeddingModel),
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
			// Near-exact duplicate: keep the existing memory and report it as
			// not created, exactly like a fingerprint match.
			existing, err := s.repo.GetByID(ctx, memory.TenantID, sr.Memory.ID)
			if err != nil {
				return nil, fmt.Errorf("load duplicate memory failed: %w", err)
			}
			if existing == nil || existing.KbID != memory.KbID {
				continue
			}
			return &types.SaveMemoryResult{Memory: existing, Created: false}, nil

		case score > s.config.SemanticDedup.NearThreshold:
			// CosineSearch returns a compact projection. Reload the complete row
			// before merging so repository.Update cannot clear omitted fields.
			existing, err := s.repo.GetByID(ctx, memory.TenantID, sr.Memory.ID)
			if err != nil {
				return nil, fmt.Errorf("load duplicate memory failed: %w", err)
			}
			if existing == nil || existing.KbID != memory.KbID {
				continue
			}
			if existing.Verdict.IsProtected() {
				continue // never rewrite a decision/fixed memory automatically
			}
			merged, err := s.mergeMemories(ctx, existing, nil, memory)
			if errors.Is(err, errMergeTooLong) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return &types.SaveMemoryResult{Memory: merged, Created: false}, nil
		}
	}

	return nil, nil
}

// memoryTermsFor returns the classification cues for the content's
// language (Vietnamese, English, ...), from the langdata registry.
func memoryTermsFor(content string) *langdata.MemoryTerms {
	return langdata.GetMemoryTerms(langdata.DetectMemoryLanguage(content))
}

// containsPhrase reports whether phrase occurs in text on word boundaries
// (so "was" does not match "wasm"). Both must be lowercase.
func containsPhrase(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(phrase)
		before, after := ' ', ' '
		if i > 0 {
			before, _ = utf8.DecodeLastRuneInString(text[:i])
		}
		if end < len(text) {
			after, _ = utf8.DecodeRuneInString(text[end:])
		}
		if !unicode.IsLetter(before) && !unicode.IsDigit(before) && !unicode.IsLetter(after) && !unicode.IsDigit(after) {
			return true
		}
		start = i + 1
	}
}

// memoryTypeOrder breaks score ties deterministically, most specific first.
var memoryTypeOrder = []string{"decision", "procedural", "preference", "episodic", "semantic"}

// detectMemoryType classifies content by the phrases of its language; with no
// cue it is a plain "fact".
func detectMemoryType(content string) string {
	lower := strings.ToLower(content)
	terms := memoryTermsFor(content)
	bestType, bestScore := "fact", 0
	for _, typ := range memoryTypeOrder {
		score := 0
		for _, kw := range terms.TypeKeywords[typ] {
			if containsPhrase(lower, kw) {
				score++
			}
		}
		if score > bestScore {
			bestType, bestScore = typ, score
		}
	}
	return bestType
}

// suggestTags extracts keyword tags from content, skipping the stopwords of
// its language.
func suggestTags(content string, memoryType string) types.TagsArray {
	stop := langdata.Get(langdata.DetectMemoryLanguage(content)).Stopwords
	words := strings.Fields(content)
	seen := make(map[string]bool)
	var tags []string

	if memoryType != "" {
		tags = append(tags, memoryType)
		seen[memoryType] = true
	}

	for _, word := range words {
		cleaned := strings.TrimFunc(strings.ToLower(word), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		n := utf8.RuneCountInString(cleaned)
		if n < 3 || n > 30 || seen[cleaned] || stop[cleaned] || isStopWord(cleaned) {
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
	return langdata.Get("en").Stopwords[word] || englishStopWords[word]
}

// englishStopWords complements langdata's English list with words that carry
// no meaning as a memory tag.
var englishStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "shall": true, "can": true, "need": true, "used": true,
	"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
	"at": true, "by": true, "from": true, "as": true, "into": true,
	"and": true, "but": true, "or": true, "not": true, "so": true,
	"this": true, "that": true, "these": true, "those": true, "it": true,
	"we": true, "they": true, "our": true, "their": true, "its": true,
	"what": true, "which": true, "who": true, "when": true, "where": true,
	"why": true, "how": true, "all": true, "each": true, "some": true,
	"than": true, "too": true, "very": true, "just": true, "about": true,
	"also": true, "if": true, "then": true, "else": true, "over": true,
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

	// Keyword bonuses, in the content's language
	lower := strings.ToLower(content)
	terms := memoryTermsFor(content)
	anyOf := func(phrases []string) bool {
		for _, p := range phrases {
			if containsPhrase(lower, p) {
				return true
			}
		}
		return false
	}
	if anyOf(terms.HighImportance) {
		score++
	}
	if anyOf(terms.DecisionSignals) {
		score += 2
	}
	if anyOf(terms.LowImportance) {
		score--
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
