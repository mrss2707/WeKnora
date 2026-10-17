package repository

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2FieldUpdater = (*MemoryRepository)(nil)

// UpdateFields writes only the given columns of one live memory. Automated
// actors cannot change a memory whose verdict is protected.
func (r *MemoryRepository) UpdateFields(ctx context.Context, tenantID, id string, fields map[string]interface{}) error {
	return r.UpdateFieldsIf(ctx, tenantID, id, fields, nil)
}

// UpdateFieldsIf is UpdateFields guarded by an optimistic lock on updated_at.
func (r *MemoryRepository) UpdateFieldsIf(
	ctx context.Context, tenantID, id string, fields map[string]interface{}, expectedUpdatedAt *time.Time,
) error {
	if len(fields) == 0 {
		return nil
	}
	if isAutomatedActor(ctx) {
		existing, err := r.GetByID(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if existing.Verdict.IsProtected() {
			return &types.ErrProtectedVerdict{}
		}
	}
	updates := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		if k == "embedding" {
			if vec, ok := v.(pgvector.Vector); ok {
				padded, err := padEmbedding(vec.Slice())
				if err != nil {
					return err
				}
				v = pgvector.NewVector(padded)
			}
		}
		updates[k] = v
	}
	updates["updated_at"] = time.Now()
	q := r.db.WithContext(ctx).Model(&types.AgentMemory{}).
		Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, id)
	if isAutomatedActor(ctx) {
		// Recheck in the UPDATE itself: a human may protect the memory
		// after the earlier read and before this statement executes.
		q = q.Where("verdict NOT IN ?", []types.MemoryVerdict{types.VerdictDecision, types.VerdictFixed})
	}
	if expectedUpdatedAt != nil {
		// Preserve PostgreSQL's full timestamp precision; millisecond
		// comparison incorrectly accepts two versions within the same ms.
		q = q.Where("date_trunc('microseconds', updated_at) = date_trunc('microseconds', ?::timestamptz)", expectedUpdatedAt.UTC())
	}
	res := q.Updates(updates)
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return &types.MemoryV2ConflictError{Message: "another memory already has this exact content"}
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		if isAutomatedActor(ctx) {
			if current, err := r.GetByID(ctx, tenantID, id); err == nil && current.Verdict.IsProtected() {
				return &types.ErrProtectedVerdict{}
			}
		}
		if expectedUpdatedAt != nil {
			if _, err := r.GetByID(ctx, tenantID, id); err == nil {
				return &types.MemoryV2ConflictError{Message: "the memory was changed by someone else; reload and try again"}
			}
		}
		return gorm.ErrRecordNotFound
	}
	r.InvalidateResultCache(ctx, tenantID)
	return nil
}

// isUniqueViolation reports a PostgreSQL unique-constraint violation.
func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "SQLSTATE 23505") || strings.Contains(err.Error(), "duplicate key value"))
}

var _ interfaces.MemoryV2AccessTracker = (*MemoryRepository)(nil)

// TouchAccess records that memories were returned to a caller: access_count
// is incremented and last_accessed_at set. updated_at is preserved (reading a
// memory is not a change; migration 900080 teaches the trigger this).
func (r *MemoryRepository) TouchAccess(ctx context.Context, tenantID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`UPDATE agent_memories
SET access_count = access_count + 1, last_accessed_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id IN ? AND deleted_at IS NULL`, tenantID, ids).Error
}
