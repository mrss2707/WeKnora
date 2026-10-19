package memory_v2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2DreamUndoer = (*MemoryServiceV2Impl)(nil)

// dreamUndoScanLimit bounds the audit rows read for one pass (a pass applies
// at most max_actions changes; a merge writes one row per participant).
const dreamUndoScanLimit = 200

// UndoDreamRun reverts the changes one Dreamer pass applied to a knowledge
// base: verdict and importance edits go back to their before values and
// merges are undone. A memory changed by anyone after the pass is left as it
// is and reported, so an undo never overwrites later work. Running it again
// reverts nothing more. The caller has authorized (tenantID, kbID) as Admin.
func (s *MemoryServiceV2Impl) UndoDreamRun(ctx context.Context, tenantID, kbID, runID string) (*types.DreamUndoResult, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2: %s", reason)
	}
	runID = strings.TrimSpace(runID)
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(kbID) == "" || runID == "" {
		return nil, &ErrMemoryValidation{Message: "kb_id and run_id are required"}
	}
	auditor, ok := s.repo.(interfaces.MemoryV2Auditor)
	if !ok {
		return nil, errors.New("memory_v2: undo is not supported by this repository")
	}
	entries, _, err := auditor.ListAudit(ctx, types.MemoryAuditFilter{
		TenantID: tenantID, KbID: kbID, DreamRun: runID, Limit: dreamUndoScanLimit,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, &ErrMemoryValidation{Message: "this Dreamer pass changed nothing in this knowledge base"}
	}
	ctx = context.WithValue(ctx, types.DreamUndoKey{}, runID)
	out := &types.DreamUndoResult{RunID: runID, Skipped: []types.DreamUndoSkip{}}
	// Newest first, so a memory touched twice is restored in reverse order.
	for _, entry := range entries {
		if entry.Actor != "dreamer" || entry.Action == types.MemoryAuditMergedIn {
			continue // losers are restored with their survivor
		}
		skip := func(reason string) {
			out.Skipped = append(out.Skipped, types.DreamUndoSkip{MemoryID: entry.MemoryID, Action: entry.Action, Reason: reason})
		}
		if reason := s.dreamEntryIsLatest(ctx, auditor, entry, runID); reason != "" {
			skip(reason)
			continue
		}
		var err error
		switch entry.Action {
		case types.MemoryAuditPatched:
			err = s.revertDreamPatch(ctx, tenantID, entry)
		case types.MemoryAuditMerged:
			_, err = s.UndoMerge(ctx, tenantID, kbID, entry.MemoryID)
		default:
			skip(types.DreamUndoSkipUnsupported)
			continue
		}
		if err != nil {
			logger.Warnf(ctx, "[MemoryV2] undo dream run %s: %s on %s: %v", runID, entry.Action, entry.MemoryID, err)
			reason := types.DreamUndoSkipFailed
			var conflict *types.MemoryV2ConflictError
			if errors.Is(err, errDreamChanged) || errors.As(err, &conflict) {
				reason = types.DreamUndoSkipChanged
			}
			skip(reason)
			continue
		}
		out.Reverted++
	}
	return out, nil
}

var errDreamChanged = errors.New("memory changed after the dreamer pass")

// dreamEntryIsLatest returns "" when entry is still the latest change of its
// memory, else why it must be left alone.
func (s *MemoryServiceV2Impl) dreamEntryIsLatest(ctx context.Context, auditor interfaces.MemoryV2Auditor, entry *types.MemoryAuditEntry, runID string) string {
	latest, _, err := auditor.ListAudit(ctx, types.MemoryAuditFilter{
		TenantID: entry.TenantID, KbID: entry.KbID, MemoryID: entry.MemoryID, Limit: 1,
	})
	if err != nil || len(latest) == 0 {
		return types.DreamUndoSkipFailed
	}
	if latest[0].ID == entry.ID {
		return ""
	}
	var details map[string]interface{}
	_ = json.Unmarshal(latest[0].Details, &details)
	if undo, _ := details[auditDreamUndoKey].(string); undo == runID {
		return types.DreamUndoSkipAlreadyDone
	}
	return types.DreamUndoSkipChanged
}

// revertDreamPatch restores the verdict and importance recorded before a
// Dreamer edit. The audit image must still match the stored row, and the
// write is a compare-and-set on its current version.
func (s *MemoryServiceV2Impl) revertDreamPatch(ctx context.Context, tenantID string, entry *types.MemoryAuditEntry) error {
	before, err := memoryFromImage(tenantID, entry.Before)
	if err != nil || len(entry.After) == 0 {
		return fmt.Errorf("memory_v2: incomplete dreamer audit entry %d", entry.ID)
	}
	after, err := memoryFromImage(tenantID, entry.After)
	if err != nil {
		return fmt.Errorf("memory_v2: incomplete dreamer audit entry %d", entry.ID)
	}
	current, err := s.repo.GetByID(ctx, tenantID, entry.MemoryID)
	if err != nil {
		return err
	}
	if current == nil || current.Verdict != after.Verdict || current.Importance != after.Importance || current.Content != after.Content {
		return errDreamChanged
	}
	patch := interfaces.MemoryV2Patch{ExpectedUpdatedAt: &current.UpdatedAt}
	if before.Verdict != after.Verdict {
		patch.Verdict = &before.Verdict
	}
	if before.Importance != after.Importance {
		patch.Importance = &before.Importance
	}
	if patch.IsEmpty() {
		return nil
	}
	_, err = s.PatchMemory(ctx, tenantID, entry.MemoryID, patch)
	return err
}
