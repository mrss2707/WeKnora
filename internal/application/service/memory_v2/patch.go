package memory_v2

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2Patcher = (*MemoryServiceV2Impl)(nil)

// Valid memory metadata ranges, shared with ingestion scoring.
const (
	minImportance = -5
	maxImportance = 6
	maxTags       = 20
	maxTagRunes   = 64
)

var validVerdicts = map[types.MemoryVerdict]bool{
	types.VerdictNone: true, types.VerdictFixed: true, types.VerdictRefuted: true,
	types.VerdictDecision: true, types.VerdictGotcha: true, types.VerdictWIP: true,
}

var validMemoryTypes = map[string]bool{
	"episodic": true, "semantic": true, "procedural": true,
	"decision": true, "preference": true, "fact": true,
}

// PatchMemory applies a partial update to one memory. Metadata changes are
// written as-is; a content change is validated, re-embedded with the
// knowledge base's model and re-fingerprinted (a collision with another
// memory is a conflict). Neither path re-runs ingestion classification. With
// ExpectedUpdatedAt the write is an optimistic-locked compare-and-set. The
// caller has already authorized tenantID for the memory's knowledge base.
func (s *MemoryServiceV2Impl) PatchMemory(
	ctx context.Context, tenantID, id string, patch interfaces.MemoryV2Patch,
) (*types.AgentMemory, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2 not ready: %s", reason)
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(id) == "" {
		return nil, &ErrMemoryValidation{Message: "tenant_id and id are required"}
	}
	fields, err := patchFields(patch)
	if err != nil {
		return nil, err
	}
	_, ok := s.repo.(interfaces.MemoryV2FieldUpdater)
	if !ok {
		return nil, fmt.Errorf("memory V2: repository does not support partial updates")
	}
	before, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, gorm.ErrRecordNotFound
	}

	if patch.Content != nil && *patch.Content != before.Content {
		if err := s.contentFields(ctx, before, *patch.Content, fields); err != nil {
			return nil, err
		}
	}
	if len(fields) == 0 {
		return before, nil
	}
	after, err := s.writeAuditedFields(ctx, before, fields, patch.ExpectedUpdatedAt, map[string]interface{}{"fields": fieldNames(fields)})
	if err != nil {
		return nil, err
	}
	if _, changed := fields["content"]; changed && s.autoLinker != nil {
		s.autoLinker.LinkMemory(ctx, after)
	}
	return after, nil
}

// contentFields validates new content and adds content, embedding,
// fingerprint and embedding-model columns to fields.
func (s *MemoryServiceV2Impl) contentFields(
	ctx context.Context, m *types.AgentMemory, content string, fields map[string]interface{},
) error {
	if err := validateContent(content); err != nil {
		return err
	}
	fp := computeScopedFingerprint(m.TenantID, m.KbID, content)
	if other, err := s.repo.GetByFingerprint(ctx, m.TenantID, fp); err != nil {
		return fmt.Errorf("fingerprint lookup failed: %w", err)
	} else if other != nil && other.ID != m.ID {
		return &types.MemoryV2ConflictError{Message: "another memory already has this exact content", ExistingID: other.ID}
	}
	embedder, modelID, err := s.embedderFor(ctx, m.TenantID, m.KbID)
	if err != nil {
		return &types.MemoryV2ModelUnavailable{Cause: err}
	}
	vec, err := embedder.Embed(ctx, content)
	if err != nil {
		return &types.MemoryV2ModelUnavailable{Cause: err}
	}
	fields["content"] = content
	fields["fingerprint"] = fp
	fields["embedding"] = pgvector.NewVector(vec)
	if modelID != "" {
		fields["metadata"] = setMetadataString(m.Metadata, metadataEmbeddingModel, modelID)
	}
	return nil
}

func fieldNames(fields map[string]interface{}) []string {
	names := make([]string, 0, len(fields))
	for k := range fields {
		if k == "embedding" || k == "fingerprint" || k == "metadata" {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// patchFields validates a patch and maps it to column updates.
func patchFields(p interfaces.MemoryV2Patch) (map[string]interface{}, error) {
	fields := map[string]interface{}{}
	if p.Verdict != nil {
		if !validVerdicts[*p.Verdict] {
			return nil, &ErrMemoryValidation{Message: fmt.Sprintf("invalid verdict %q", *p.Verdict)}
		}
		fields["verdict"] = string(*p.Verdict)
	}
	if p.Importance != nil {
		if *p.Importance < minImportance || *p.Importance > maxImportance {
			return nil, &ErrMemoryValidation{Message: fmt.Sprintf("importance must be between %d and %d", minImportance, maxImportance)}
		}
		fields["importance"] = *p.Importance
	}
	if p.Tier != nil {
		if *p.Tier < 0 || *p.Tier > 3 {
			return nil, &ErrMemoryValidation{Message: "tier must be between 0 and 3"}
		}
		fields["tier"] = *p.Tier
	}
	if p.MemoryType != nil {
		if !validMemoryTypes[*p.MemoryType] {
			return nil, &ErrMemoryValidation{Message: fmt.Sprintf("invalid memory_type %q", *p.MemoryType)}
		}
		fields["memory_type"] = *p.MemoryType
	}
	if p.Tags != nil {
		tags, err := normalizeTags(*p.Tags)
		if err != nil {
			return nil, err
		}
		fields["tags"] = tags
	}
	return fields, nil
}

// normalizeTags trims, de-duplicates (case-insensitively) and bounds tags.
func normalizeTags(in []string) (types.TagsArray, error) {
	out := types.TagsArray{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len([]rune(t)) > maxTagRunes {
			return nil, &ErrMemoryValidation{Message: fmt.Sprintf("tag %q exceeds %d characters", truncateRunes(t, 20), maxTagRunes)}
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, &ErrMemoryValidation{Message: fmt.Sprintf("at most %d tags are allowed", maxTags)}
	}
	return out, nil
}

var (
	_ interfaces.MemoryV2Deleter       = (*MemoryServiceV2Impl)(nil)
	_ interfaces.MemoryV2Undoer        = (*MemoryServiceV2Impl)(nil)
	_ interfaces.MemoryV2HistoryReader = (*MemoryServiceV2Impl)(nil)
)

// DeleteMemory soft-deletes a memory and its relations and audits it.
func (s *MemoryServiceV2Impl) DeleteMemory(ctx context.Context, tenantID, id string) error {
	if reason := s.notReadyReason(); reason != "" {
		return fmt.Errorf("memory V2 not ready: %s", reason)
	}
	before, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if before == nil {
		return gorm.ErrRecordNotFound
	}
	if atomic, ok := s.repo.(interfaces.MemoryV2AtomicDeleter); ok {
		return atomic.DeleteAndAudit(ctx, tenantID, id, func(lockedBefore, _ *types.AgentMemory) *types.MemoryAuditEntry {
			return newAudit(ctx, types.MemoryAuditDeleted, lockedBefore, nil, nil)
		})
	}
	if err := s.repo.Delete(ctx, tenantID, id); err != nil {
		return err
	}
	s.recordAudit(ctx, newAudit(ctx, types.MemoryAuditDeleted, before, nil, nil))
	return nil
}

// History lists the audit trail of a knowledge base (or one memory).
func (s *MemoryServiceV2Impl) History(ctx context.Context, f types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error) {
	auditor, ok := s.repo.(interfaces.MemoryV2Auditor)
	if !ok {
		return []*types.MemoryAuditEntry{}, 0, nil
	}
	if strings.TrimSpace(f.TenantID) == "" {
		return nil, 0, &ErrMemoryValidation{Message: "tenant_id is required"}
	}
	return auditor.ListAudit(ctx, f)
}
