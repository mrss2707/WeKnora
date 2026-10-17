package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ interfaces.MemoryV2VersionedMergeStore = (*MemoryRepository)(nil)

type mergeRelationSnapshot struct {
	Restore []string `json:"restore_relation_ids"`
	Remove  []string `json:"remove_relation_ids"`
}

func lockMergeRows(tx *gorm.DB, tenantID string, ids []string, unscoped bool) (map[string]*types.AgentMemory, error) {
	slices.Sort(ids) // consistent lock order for overlapping merges
	var rows []*types.AgentMemory
	q := tx.Where("tenant_id = ? AND id IN ?", tenantID, ids).Order("id").Clauses(clause.Locking{Strength: "UPDATE"})
	if unscoped {
		q = q.Unscoped()
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	result := map[string]*types.AgentMemory{}
	for _, m := range rows {
		result[m.ID] = m
	}
	return result, nil
}

func checkMergeVersion(current, expected *types.AgentMemory) error {
	if current == nil || expected == nil || current.TenantID != expected.TenantID || current.KbID != expected.KbID || !current.UpdatedAt.Truncate(time.Microsecond).Equal(expected.UpdatedAt.Truncate(time.Microsecond)) {
		return &types.MemoryV2ConflictError{Message: "a merge participant changed; reload and try again"}
	}
	return nil
}

func (r *MemoryRepository) MergeIntoVersioned(ctx context.Context, before, after *types.AgentMemory, losers []*types.AgentMemory, audit []*types.MemoryAuditEntry) error {
	if before == nil || after == nil || before.ID != after.ID || before.TenantID != after.TenantID || before.KbID != after.KbID || len(audit) == 0 {
		return fmt.Errorf("invalid versioned merge")
	}
	ids := []string{before.ID}
	loserIDs := []string{}
	seen := map[string]bool{before.ID: true}
	for _, m := range losers {
		if m == nil || seen[m.ID] || m.TenantID != before.TenantID || m.KbID != before.KbID {
			return fmt.Errorf("invalid merge participant")
		}
		seen[m.ID] = true
		ids = append(ids, m.ID)
		loserIDs = append(loserIDs, m.ID)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockMergeRows(tx, before.TenantID, ids, false)
		if err != nil {
			return err
		}
		if err := checkMergeVersion(locked[before.ID], before); err != nil {
			return err
		}
		if isAutomatedActor(ctx) && locked[before.ID].Verdict.IsProtected() {
			return &types.ErrProtectedVerdict{}
		}
		for _, m := range losers {
			current := locked[m.ID]
			if current.Verdict.IsProtected() {
				return &types.ErrProtectedVerdict{}
			}
			if err := checkMergeVersion(current, m); err != nil {
				return err
			}
		}
		// Lock and snapshot live relations so undo can restore the original graph,
		// not just undelete the text while leaving all edges moved to the survivor.
		var edges []*types.MemoryRelation
		if err := tx.Where("tenant_id = ? AND deleted_at IS NULL AND (from_uuid IN ? OR to_uuid IN ?)", before.TenantID, ids, ids).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&edges).Error; err != nil {
			return err
		}
		snapshot := mergeRelationSnapshot{Restore: []string{}, Remove: []string{}}
		prior := map[string]bool{}
		losersSet := map[string]bool{}
		for _, id := range loserIDs {
			losersSet[id] = true
		}
		for _, edge := range edges {
			prior[edge.ID] = true
			if losersSet[edge.FromUUID] || losersSet[edge.ToUUID] {
				snapshot.Restore = append(snapshot.Restore, edge.ID)
			}
		}
		scoped := NewMemoryRepository(tx)
		if err := scoped.MergeInto(ctx, after, loserIDs, nil); err != nil {
			return err
		}
		var newEdges []*types.MemoryRelation
		if err := tx.Where("tenant_id = ? AND deleted_at IS NULL AND (from_uuid = ? OR to_uuid = ?)", before.TenantID, before.ID, before.ID).Find(&newEdges).Error; err != nil {
			return err
		}
		for _, edge := range newEdges {
			if !prior[edge.ID] {
				snapshot.Remove = append(snapshot.Remove, edge.ID)
			}
		}
		for _, entry := range audit {
			if entry == nil || entry.MemoryID != before.ID || entry.Action != types.MemoryAuditMerged {
				continue
			}
			details := map[string]interface{}{}
			if len(entry.Details) > 0 {
				if err := json.Unmarshal(entry.Details, &details); err != nil {
					return err
				}
			}
			if details == nil {
				details = map[string]interface{}{}
			}
			details["restore_relation_ids"], details["remove_relation_ids"] = snapshot.Restore, snapshot.Remove
			entry.Details, err = json.Marshal(details)
			if err != nil {
				return err
			}
		}
		return recordAudit(tx, audit)
	})
	if err == nil {
		r.InvalidateResultCache(ctx, before.TenantID)
	}
	return err
}

func (r *MemoryRepository) RestoreMergeVersioned(ctx context.Context, current, before *types.AgentMemory, losers []*types.AgentMemory, mergeDetails []byte, expectedAuditID int64, audit []*types.MemoryAuditEntry) error {
	if current == nil || before == nil || current.ID != before.ID || current.TenantID != before.TenantID || current.KbID != before.KbID {
		return fmt.Errorf("invalid versioned merge undo")
	}
	ids := []string{current.ID}
	loserIDs := []string{}
	for _, m := range losers {
		if m == nil || m.ID == current.ID || m.TenantID != current.TenantID || m.KbID != current.KbID {
			return fmt.Errorf("invalid undo participant")
		}
		ids = append(ids, m.ID)
		loserIDs = append(loserIDs, m.ID)
	}
	var snapshot mergeRelationSnapshot
	if len(mergeDetails) > 0 {
		if err := json.Unmarshal(mergeDetails, &snapshot); err != nil {
			return err
		}
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockMergeRows(tx, current.TenantID, ids, true)
		if err != nil {
			return err
		}
		if err := checkMergeVersion(locked[current.ID], current); err != nil {
			return err
		}
		for _, id := range loserIDs {
			m := locked[id]
			if m.KbID != before.KbID || !m.DeletedAt.Valid {
				return &types.MemoryV2ConflictError{Message: "a merged memory changed; undo is no longer safe"}
			}
		}
		var latest types.MemoryAuditEntry
		if err := tx.Where("tenant_id = ? AND memory_id = ? AND action IN ?", current.TenantID, current.ID, []string{types.MemoryAuditMerged, types.MemoryAuditPatched, types.MemoryAuditUnmerged}).Order("id DESC").First(&latest).Error; err != nil {
			return err
		}
		if latest.ID != expectedAuditID || latest.Action != types.MemoryAuditMerged {
			return &types.MemoryV2ConflictError{Message: "the latest change is no longer this merge"}
		}
		if err := NewMemoryRepository(tx).RestoreMerge(ctx, before, losers, audit); err != nil {
			return err
		}
		if len(snapshot.Remove) > 0 {
			if err := tx.Where("tenant_id = ? AND id IN ? AND (from_uuid = ? OR to_uuid = ?)", current.TenantID, snapshot.Remove, current.ID, current.ID).Delete(&types.MemoryRelation{}).Error; err != nil {
				return err
			}
		}
		if len(snapshot.Restore) > 0 && len(loserIDs) > 0 {
			if err := tx.Exec(`UPDATE memory_relations r SET deleted_at = NULL
    WHERE r.tenant_id = ? AND r.id IN ? AND (r.from_uuid IN ? OR r.to_uuid IN ?)
      AND NOT EXISTS (SELECT 1 FROM memory_relations live WHERE live.tenant_id=r.tenant_id AND live.from_uuid=r.from_uuid AND live.to_uuid=r.to_uuid AND live.relation_type=r.relation_type AND live.deleted_at IS NULL)
      AND EXISTS (SELECT 1 FROM agent_memories m WHERE m.id=r.from_uuid AND m.tenant_id=? AND m.kb_id=? AND m.deleted_at IS NULL)
      AND EXISTS (SELECT 1 FROM agent_memories m WHERE m.id=r.to_uuid AND m.tenant_id=? AND m.kb_id=? AND m.deleted_at IS NULL)`, current.TenantID, snapshot.Restore, loserIDs, loserIDs, current.TenantID, current.KbID, current.TenantID, current.KbID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		r.InvalidateResultCache(ctx, current.TenantID)
	}
	return err
}
