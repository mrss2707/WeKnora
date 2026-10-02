package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2KBScopeRepository = (*MemoryRepository)(nil)

// FindByID loads a live memory without a tenant filter.
func (r *MemoryRepository) FindByID(ctx context.Context, id string) (*types.AgentMemory, error) {
	var m types.AgentMemory
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListKBScopeMismatches finds memories whose tenant differs from the owner
// tenant of their knowledge base. Memories without a KB, or whose KB no longer
// exists, are not repairable and are ignored.
func (r *MemoryRepository) ListKBScopeMismatches(ctx context.Context, limit int) ([]*interfaces.MemoryKBScopeMismatch, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	type row struct {
		MemoryID       string
		KbID           string
		CurrentTenant  string
		OwnerTenant    string
		Content        string
		OldFingerprint *string
	}
	var rows []row
	q := r.db.WithContext(ctx).Table("agent_memories AS m").
		Select("m.id AS memory_id, m.kb_id, m.tenant_id AS current_tenant, " +
			"CAST(kb.tenant_id AS TEXT) AS owner_tenant, m.content, m.fingerprint AS old_fingerprint").
		Joins("JOIN knowledge_bases kb ON kb.id = m.kb_id AND kb.deleted_at IS NULL").
		Where("m.deleted_at IS NULL").
		Where("m.tenant_id <> CAST(kb.tenant_id AS TEXT)").
		Order("m.created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*interfaces.MemoryKBScopeMismatch, len(rows))
	for i, rw := range rows {
		out[i] = &interfaces.MemoryKBScopeMismatch{
			MemoryID: rw.MemoryID, KbID: rw.KbID, CurrentTenant: rw.CurrentTenant,
			OwnerTenant: rw.OwnerTenant, Content: rw.Content, OldFingerprint: rw.OldFingerprint,
		}
	}
	return out, nil
}

// MoveToKBOwnerTenant moves one memory (and its queued extraction jobs) to the
// KB owner's tenant and records the move in agent_memories_scope_repair_log.
func (r *MemoryRepository) MoveToKBOwnerTenant(ctx context.Context, m *interfaces.MemoryKBScopeMismatch, newFingerprint string) error {
	if m == nil || m.MemoryID == "" || m.OwnerTenant == "" {
		return errors.New("memory_v2: invalid scope repair request")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fp *string
		if newFingerprint != "" {
			var clash int64
			if err := tx.Table("agent_memories").
				Where("fingerprint = ? AND id <> ? AND deleted_at IS NULL", newFingerprint, m.MemoryID).
				Count(&clash).Error; err != nil {
				return err
			}
			if clash == 0 {
				fp = &newFingerprint
			}
		}
		res := tx.Exec(
			"UPDATE agent_memories SET tenant_id = ?, fingerprint = ? "+
				"WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL",
			m.OwnerTenant, fp, m.MemoryID, m.CurrentTenant)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // already repaired concurrently
		}
		if err := tx.Exec("UPDATE extraction_queue SET tenant_id = ? WHERE memory_uuid = ?",
			m.OwnerTenant, m.MemoryID).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO agent_memories_scope_repair_log "+
				"(memory_id, kb_id, old_tenant_id, new_tenant_id, old_fingerprint, new_fingerprint) "+
				"VALUES (?, ?, ?, ?, ?, ?)",
			m.MemoryID, m.KbID, m.CurrentTenant, m.OwnerTenant, m.OldFingerprint, fp).Error
	})
}

// SyncRelationTenants aligns relation tenants with their endpoints after a
// repair. A relation is only meaningful when both endpoints share a tenant;
// the rest cannot be traversed any more and are soft-deleted.
func (r *MemoryRepository) SyncRelationTenants(ctx context.Context, memoryIDs []string) (int64, int64, error) {
	if len(memoryIDs) == 0 {
		return 0, 0, nil
	}
	var retenanted, dropped int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`
UPDATE memory_relations r SET tenant_id = f.tenant_id
FROM agent_memories f, agent_memories t
WHERE f.id = r.from_uuid AND t.id = r.to_uuid
  AND f.tenant_id = t.tenant_id AND r.tenant_id <> f.tenant_id
  AND r.deleted_at IS NULL
  AND (r.from_uuid IN ? OR r.to_uuid IN ?)`, memoryIDs, memoryIDs)
		if res.Error != nil {
			return res.Error
		}
		retenanted = res.RowsAffected
		res = tx.Exec(`
UPDATE memory_relations r SET deleted_at = CURRENT_TIMESTAMP
FROM agent_memories f, agent_memories t
WHERE f.id = r.from_uuid AND t.id = r.to_uuid
  AND f.tenant_id <> t.tenant_id
  AND r.deleted_at IS NULL
  AND (r.from_uuid IN ? OR r.to_uuid IN ?)`, memoryIDs, memoryIDs)
		dropped = res.RowsAffected
		return res.Error
	})
	return retenanted, dropped, err
}
