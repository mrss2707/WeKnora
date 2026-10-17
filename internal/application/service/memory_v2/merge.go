package memory_v2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
)

// errMergeTooLong signals that a merge was skipped because the merged content
// would exceed the cap. Merges never truncate.
var errMergeTooLong = errors.New("merged content exceeds cap")

// mergeSeparator joins merged contents.
const mergeSeparator = "\n---\n"

// mergeCap returns the merged-content cap in characters.
func (s *MemoryServiceV2Impl) mergeCap() int {
	c := s.config.SemanticDedup.MergeMaxChars
	if c <= 0 || c > maxContentChars {
		return maxContentChars
	}
	return c
}

// mergeMemories folds additions into survivor and persists the result
// atomically. losers are existing memories that are soft-deleted (their
// relations move to the survivor); extra is content of a memory that was
// never stored (an incoming near-duplicate). Every stored memory involved is
// audited with its before image, so the merge can be undone.
func (s *MemoryServiceV2Impl) mergeMemories(
	ctx context.Context, survivor *types.AgentMemory, losers []*types.AgentMemory, extra *types.AgentMemory,
) (*types.AgentMemory, error) {
	if survivor == nil {
		return nil, errors.New("memory_v2: merge needs a survivor")
	}
	for _, l := range losers {
		if l == nil || l.TenantID != survivor.TenantID || l.KbID != survivor.KbID {
			return nil, errors.New("memory_v2: merge across tenants or knowledge bases is not allowed")
		}
		if l.Verdict.IsProtected() {
			return nil, &types.ErrProtectedVerdict{}
		}
	}

	before := *survivor
	before.Tags = append(types.TagsArray(nil), survivor.Tags...)
	merged := *survivor

	parts := []string{survivor.Content}
	// A part already present (e.g. the same fact saved again after an earlier
	// merge) is not appended twice.
	seen := map[string]bool{}
	for _, p := range strings.Split(survivor.Content, mergeSeparator) {
		seen[strings.TrimSpace(p)] = true
	}
	tags := append([]string(nil), survivor.Tags...)
	mergedFrom := []string{}
	add := func(m *types.AgentMemory) {
		if c := strings.TrimSpace(m.Content); !seen[c] {
			seen[c] = true
			parts = append(parts, m.Content)
		}
		tags = append(tags, m.Tags...)
		if m.Importance > merged.Importance {
			merged.Importance = m.Importance
		}
	}
	for _, l := range losers {
		add(l)
		mergedFrom = append(mergedFrom, l.ID)
	}
	if extra != nil {
		add(extra)
	}
	if len(losers) == 0 && len(parts) == 1 {
		// An incoming near-duplicate adds nothing the survivor does not
		// already say: keep the survivor untouched.
		return survivor, nil
	}
	merged.Content = strings.Join(parts, mergeSeparator)
	if utf8.RuneCountInString(merged.Content) > s.mergeCap() {
		return nil, errMergeTooLong
	}
	merged.Tags = unionTags(tags, maxTags)

	embedder, modelID, err := s.embedderFor(ctx, merged.TenantID, merged.KbID)
	if err != nil {
		return nil, &types.MemoryV2ModelUnavailable{Cause: err}
	}
	vec, err := embedder.Embed(ctx, merged.Content)
	if err != nil {
		return nil, &types.MemoryV2ModelUnavailable{Cause: err}
	}
	merged.Embedding = pgvector.NewVector(vec)
	if modelID != "" {
		merged.Metadata = setMetadataString(merged.Metadata, metadataEmbeddingModel, modelID)
	}
	if len(mergedFrom) > 0 {
		merged.Metadata = appendMetadataList(merged.Metadata, "merged_from", mergedFrom)
	}
	fp := computeScopedFingerprint(merged.TenantID, merged.KbID, merged.Content)
	if other, err := s.repo.GetByFingerprint(ctx, merged.TenantID, fp); err == nil && other != nil && other.ID != merged.ID {
		merged.Fingerprint = nil // identical text already stored elsewhere; keep both rows
	} else {
		merged.Fingerprint = &fp
	}

	details := map[string]interface{}{"losers": mergedFrom}
	if extra != nil {
		details["absorbed_new_content"] = true
	}
	audit := []*types.MemoryAuditEntry{newAudit(ctx, types.MemoryAuditMerged, &before, &merged, details)}
	loserIDs := make([]string, 0, len(losers))
	for _, l := range losers {
		loserIDs = append(loserIDs, l.ID)
		audit = append(audit, newAudit(ctx, types.MemoryAuditMergedIn, l, nil, map[string]interface{}{"survivor": merged.ID}))
	}

	store, ok := s.repo.(interfaces.MemoryV2MergeStore)
	if versioned, supportsVersion := s.repo.(interfaces.MemoryV2VersionedMergeStore); supportsVersion {
		if err := versioned.MergeIntoVersioned(ctx, &before, &merged, losers, audit); err != nil {
			return nil, fmt.Errorf("merge failed: %w", err)
		}
	} else if !ok {
		// Repositories without transactional merge support (test fakes) fall
		// back to updating the survivor and deleting the losers.
		if err := s.repo.Update(ctx, &merged); err != nil {
			return nil, fmt.Errorf("merge update failed: %w", err)
		}
		for _, id := range loserIDs {
			if err := s.repo.Delete(ctx, merged.TenantID, id); err != nil {
				return nil, fmt.Errorf("merge delete failed: %w", err)
			}
		}
		s.recordAudit(ctx, audit...)
	} else if err := store.MergeInto(ctx, &merged, loserIDs, audit); err != nil {
		return nil, fmt.Errorf("merge failed: %w", err)
	}
	*survivor = merged
	return survivor, nil
}

// unionTags de-duplicates tags case-insensitively, keeping order, and keeps
// at most limit of them.
func unionTags(in []string, limit int) types.TagsArray {
	out := types.TagsArray{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		key := strings.ToLower(t)
		if t == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
		if len(out) == limit {
			break
		}
	}
	return out
}

// appendMetadataList appends ids to a string-list metadata key.
func appendMetadataList(raw []byte, key string, ids []string) []byte {
	m := map[string]interface{}{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
		if m == nil {
			m = map[string]interface{}{}
		}
	}
	var list []interface{}
	if existing, ok := m[key].([]interface{}); ok {
		list = existing
	}
	for _, id := range ids {
		list = append(list, id)
	}
	m[key] = list
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// UndoMerge restores a merge recorded by mergeMemories: the survivor returns
// to its before image (and is re-embedded) and the losers are undeleted.
func (s *MemoryServiceV2Impl) UndoMerge(ctx context.Context, tenantID, kbID, survivorID string) (*types.AgentMemory, error) {
	auditor, ok := s.repo.(interfaces.MemoryV2Auditor)
	store, ok2 := s.repo.(interfaces.MemoryV2MergeStore)
	if !ok || !ok2 {
		return nil, errors.New("memory_v2: undo is not supported by this repository")
	}
	entries, _, err := auditor.ListAudit(ctx, types.MemoryAuditFilter{
		TenantID: tenantID, KbID: kbID, MemoryID: survivorID,
		Actions: []string{types.MemoryAuditMerged, types.MemoryAuditPatched, types.MemoryAuditUnmerged}, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || entries[0].Action != types.MemoryAuditMerged {
		return nil, &ErrMemoryValidation{Message: "the latest change of this memory is not a merge"}
	}
	last := entries[0]
	before, err := memoryFromImage(tenantID, last.Before)
	if err != nil {
		return nil, fmt.Errorf("memory_v2: corrupt merge audit: %w", err)
	}
	var details struct {
		Losers []string `json:"losers"`
	}
	_ = json.Unmarshal(last.Details, &details)
	losers := make([]*types.AgentMemory, 0, len(details.Losers))
	for _, id := range details.Losers {
		losers = append(losers, &types.AgentMemory{ID: id, TenantID: tenantID, KbID: kbID})
	}
	current, err := s.repo.GetByID(ctx, tenantID, survivorID)
	if err != nil {
		return nil, err
	}
	// Prepare the restored vector before touching storage. A model failure
	// must not commit restored text paired with the old merged embedding.
	emb, modelID, err := s.embedderFor(ctx, tenantID, kbID)
	if err != nil {
		return nil, &types.MemoryV2ModelUnavailable{Cause: err}
	}
	vec, err := emb.Embed(ctx, before.Content)
	if err != nil {
		return nil, &types.MemoryV2ModelUnavailable{Cause: err}
	}
	before.Embedding = pgvector.NewVector(vec)
	if len(before.Metadata) == 0 {
		before.Metadata = legacyUndoMetadata(current.Metadata, details.Losers)
	}
	if modelID != "" {
		before.Metadata = setMetadataString(before.Metadata, metadataEmbeddingModel, modelID)
	}
	audit := []*types.MemoryAuditEntry{newAudit(ctx, types.MemoryAuditUnmerged, current, before, map[string]interface{}{"restored": details.Losers})}
	if versioned, ok := s.repo.(interfaces.MemoryV2VersionedMergeStore); ok {
		err = versioned.RestoreMergeVersioned(ctx, current, before, losers, last.Details, last.ID, audit)
	} else {
		err = store.RestoreMerge(ctx, before, losers, audit)
	}
	if err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, tenantID, survivorID)
}

// MergeInto folds losers into survivor through the audited merge transaction
// (the dreamer's merge path). Implements workers.DreamApplier.
func (s *MemoryServiceV2Impl) MergeInto(ctx context.Context, survivor *types.AgentMemory, losers []*types.AgentMemory) (*types.AgentMemory, error) {
	return s.mergeMemories(ctx, survivor, losers, nil)
}
