package memory_v2

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
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

// PatchMemory applies a metadata patch to one memory without re-running
// ingestion (no re-embedding, dedup or re-classification), so a verdict or
// importance change never rewrites the memory. The caller has already
// authorized tenantID for the memory's knowledge base.
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
	if len(fields) > 0 {
		updater, ok := s.repo.(interfaces.MemoryV2FieldUpdater)
		if !ok {
			return nil, fmt.Errorf("memory V2: repository does not support partial updates")
		}
		if err := updater.UpdateFields(ctx, tenantID, id, fields); err != nil {
			return nil, err
		}
	}
	return s.repo.GetByID(ctx, tenantID, id)
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
