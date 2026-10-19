package memory_v2

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// auditImage is the stored before/after state of a memory. The embedding is
// left out (large and re-derivable from content).
type auditImage struct {
	ID          string              `json:"id"`
	KbID        string              `json:"kb_id"`
	UserID      string              `json:"user_id,omitempty"`
	Content     string              `json:"content"`
	MemoryType  string              `json:"memory_type"`
	Importance  int                 `json:"importance"`
	Tier        int                 `json:"tier"`
	Verdict     types.MemoryVerdict `json:"verdict"`
	Tags        []string            `json:"tags"`
	Fingerprint *string             `json:"fingerprint,omitempty"`
	SessionID   string              `json:"session_id,omitempty"`
	Metadata    json.RawMessage     `json:"metadata,omitempty"`
}

func imageOf(m *types.AgentMemory) []byte {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(auditImage{
		ID: m.ID, KbID: m.KbID, UserID: m.UserID, Content: m.Content, MemoryType: m.MemoryType,
		Importance: m.Importance, Tier: m.Tier, Verdict: m.Verdict, Tags: append([]string(nil), m.Tags...),
		Fingerprint: m.Fingerprint, SessionID: m.SessionID, Metadata: m.Metadata,
	})
	if err != nil {
		return nil
	}
	return b
}

// memoryFromImage rebuilds the audited fields of a memory from an image.
func memoryFromImage(tenantID string, raw []byte) (*types.AgentMemory, error) {
	var img auditImage
	if err := json.Unmarshal(raw, &img); err != nil {
		return nil, err
	}
	return &types.AgentMemory{
		ID: img.ID, TenantID: tenantID, KbID: img.KbID, UserID: img.UserID, Content: img.Content,
		MemoryType: img.MemoryType, Importance: img.Importance, Tier: img.Tier, Verdict: img.Verdict,
		Tags: types.TagsArray(img.Tags), Fingerprint: img.Fingerprint, SessionID: img.SessionID, Metadata: img.Metadata,
	}, nil
}

// auditActor names who performed a change: the automated worker when the
// context says so, otherwise the human caller (or the generic "user").
func auditActor(ctx context.Context) (actor, userID string) {
	userID = types.HumanUserIDOrEmpty(ctx)
	if a, ok := ctx.Value(types.ActorKey{}).(string); ok && a != "" {
		return a, userID
	}
	if id, ok := types.UserIDFromContext(ctx); ok && id != "" && userID == "" {
		return id, "" // machine principal, e.g. an MCP endpoint
	}
	return "user", userID
}

// Audit detail keys tagging the changes of one Dreamer pass and the entries
// that undo it.
const (
	auditDreamRunKey  = "dream_run"
	auditDreamUndoKey = "undo_dream_run"
)

// withDreamRun adds the Dreamer pass (or the pass being undone) of ctx to
// audit details without mutating the caller's map.
func withDreamRun(ctx context.Context, details map[string]interface{}) map[string]interface{} {
	tags := map[string]string{}
	if run := types.DreamRunFromContext(ctx); run != "" {
		tags[auditDreamRunKey] = run
	}
	if undo, _ := ctx.Value(types.DreamUndoKey{}).(string); undo != "" {
		tags[auditDreamUndoKey] = undo
	}
	if len(tags) == 0 {
		return details
	}
	out := make(map[string]interface{}, len(details)+len(tags))
	for k, v := range details {
		out[k] = v
	}
	for k, v := range tags {
		out[k] = v
	}
	return out
}

// newAudit builds an audit entry for a memory change. Changes made by a
// Dreamer pass carry its run id, so the pass can be undone as a whole.
func newAudit(ctx context.Context, action string, before, after *types.AgentMemory, details map[string]interface{}) *types.MemoryAuditEntry {
	ref := after
	if ref == nil {
		ref = before
	}
	if ref == nil {
		return nil
	}
	actor, userID := auditActor(ctx)
	e := &types.MemoryAuditEntry{
		TenantID: ref.TenantID, KbID: ref.KbID, MemoryID: ref.ID,
		Actor: actor, UserID: userID, Action: action,
		Before: imageOf(before), After: imageOf(after),
	}
	details = withDreamRun(ctx, details)
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			e.Details = b
		}
	}
	return e
}

// recordAudit stores audit entries best-effort: a failed audit write is
// logged and never fails the user's operation.
func (s *MemoryServiceV2Impl) recordAudit(ctx context.Context, entries ...*types.MemoryAuditEntry) {
	auditor, ok := s.repo.(interfaces.MemoryV2Auditor)
	if !ok {
		return
	}
	if err := auditor.RecordAudit(ctx, entries...); err != nil {
		logger.Warnf(ctx, "[MemoryV2] audit write failed: %v", err)
	}
}
