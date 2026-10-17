package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

var (
	_ interfaces.MemoryV2Auditor    = (*MemoryRepository)(nil)
	_ interfaces.MemoryV2MergeStore = (*MemoryRepository)(nil)
)

// mergeColumns are the survivor columns a merge rewrites.
var mergeColumns = []string{"content", "importance", "embedding", "fingerprint", "tags", "metadata", "updated_at"}

// RecordAudit appends audit rows.
func (r *MemoryRepository) RecordAudit(ctx context.Context, entries ...*types.MemoryAuditEntry) error {
	return recordAudit(r.db.WithContext(ctx), entries)
}

func recordAudit(tx *gorm.DB, entries []*types.MemoryAuditEntry) error {
	rows := make([]*types.MemoryAuditEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now()
		}
		if len(e.Details) == 0 {
			e.Details = []byte("{}")
		}
		rows = append(rows, e)
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

// ListAudit returns audit rows of one scope (and optionally one memory),
// newest first, with the total count.
func (r *MemoryRepository) ListAudit(ctx context.Context, f types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.MemoryAuditEntry{}).Where("tenant_id = ?", f.TenantID)
	if f.KbID != "" {
		q = q.Where("kb_id = ?", f.KbID)
	}
	if f.MemoryID != "" {
		q = q.Where("memory_id = ?", f.MemoryID)
	}
	if len(f.Actions) > 0 {
		q = q.Where("action IN ?", f.Actions)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	var rows []*types.MemoryAuditEntry
	err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// MergeInto writes the merged survivor, soft-deletes the losers, moves their
// relations onto the survivor and records the audit rows atomically.
func (r *MemoryRepository) MergeInto(
	ctx context.Context, survivor *types.AgentMemory, loserIDs []string, audit []*types.MemoryAuditEntry,
) error {
	if survivor == nil || survivor.ID == "" {
		return errors.New("memory_v2: merge needs a survivor")
	}
	padded, err := padEmbedding(survivor.Embedding.Slice())
	if err != nil {
		return err
	}
	survivor.Embedding = pgvector.NewVector(padded)
	survivor.UpdatedAt = time.Now()

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&types.AgentMemory{}).
			Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", survivor.TenantID, survivor.ID).
			Select(mergeColumns).Updates(survivor)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if len(loserIDs) > 0 {
			if err := repointRelations(tx, survivor.TenantID, survivor.ID, loserIDs); err != nil {
				return err
			}
			if err := tx.Where("tenant_id = ? AND id IN ?", survivor.TenantID, loserIDs).
				Delete(&types.AgentMemory{}).Error; err != nil {
				return err
			}
		}
		return recordAudit(tx, audit)
	})
	if err == nil {
		r.InvalidateResultCache(ctx, survivor.TenantID)
	}
	return err
}

// repointRelations copies the losers' live relations onto the survivor
// (skipping duplicates and self-loops) and soft-deletes the originals.
func repointRelations(tx *gorm.DB, tenantID, survivorID string, loserIDs []string) error {
	// Outgoing edges: loser -> X becomes survivor -> X.
	if err := tx.Exec(`
INSERT INTO memory_relations (id, tenant_id, from_uuid, to_uuid, relation_type, weight, metadata, created_at)
SELECT gen_random_uuid()::text, r.tenant_id, ?, r.to_uuid, r.relation_type, r.weight, r.metadata, CURRENT_TIMESTAMP
FROM memory_relations r
WHERE r.tenant_id = ? AND r.deleted_at IS NULL AND r.from_uuid IN ?
  AND r.to_uuid <> ? AND r.to_uuid NOT IN ?
ON CONFLICT DO NOTHING`, survivorID, tenantID, loserIDs, survivorID, loserIDs).Error; err != nil {
		return err
	}
	// Incoming edges: X -> loser becomes X -> survivor.
	if err := tx.Exec(`
INSERT INTO memory_relations (id, tenant_id, from_uuid, to_uuid, relation_type, weight, metadata, created_at)
SELECT gen_random_uuid()::text, r.tenant_id, r.from_uuid, ?, r.relation_type, r.weight, r.metadata, CURRENT_TIMESTAMP
FROM memory_relations r
WHERE r.tenant_id = ? AND r.deleted_at IS NULL AND r.to_uuid IN ?
  AND r.from_uuid <> ? AND r.from_uuid NOT IN ?
ON CONFLICT DO NOTHING`, survivorID, tenantID, loserIDs, survivorID, loserIDs).Error; err != nil {
		return err
	}
	return softDeleteRelationsOf(tx, tenantID, loserIDs)
}

// softDeleteRelationsOf soft-deletes every live relation touching the ids.
func softDeleteRelationsOf(tx *gorm.DB, tenantID string, ids []string) error {
	return tx.Exec(`UPDATE memory_relations SET deleted_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND deleted_at IS NULL AND (from_uuid IN ? OR to_uuid IN ?)`, tenantID, ids, ids).Error
}

// RestoreMerge reverts a merge: the survivor's merged columns return to their
// before-image and the losers are undeleted. Relations moved by the merge stay
// on the survivor (they remain valid edges).
func (r *MemoryRepository) RestoreMerge(
	ctx context.Context, survivorBefore *types.AgentMemory, losers []*types.AgentMemory, audit []*types.MemoryAuditEntry,
) error {
	if survivorBefore == nil || survivorBefore.ID == "" {
		return errors.New("memory_v2: restore needs the survivor before-image")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"content":     survivorBefore.Content,
			"importance":  survivorBefore.Importance,
			"fingerprint": survivorBefore.Fingerprint,
			"tags":        survivorBefore.Tags,
			"updated_at":  time.Now(),
		}
		if len(survivorBefore.Embedding.Slice()) > 0 {
			padded, err := padEmbedding(survivorBefore.Embedding.Slice())
			if err != nil {
				return err
			}
			updates["embedding"] = pgvector.NewVector(padded)
		}
		if len(survivorBefore.Metadata) > 0 {
			updates["metadata"] = survivorBefore.Metadata
		}
		res := tx.Model(&types.AgentMemory{}).
			Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", survivorBefore.TenantID, survivorBefore.ID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		for _, l := range losers {
			if l == nil || l.ID == "" {
				continue
			}
			if err := tx.Unscoped().Model(&types.AgentMemory{}).
				Where("tenant_id = ? AND id = ?", survivorBefore.TenantID, l.ID).
				Update("deleted_at", nil).Error; err != nil {
				return err
			}
		}
		return recordAudit(tx, audit)
	})
	if err == nil {
		r.InvalidateResultCache(ctx, survivorBefore.TenantID)
	}
	return err
}
