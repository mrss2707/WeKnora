package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const kbScopeExtraDDL = `
CREATE TABLE knowledge_bases (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    deleted_at TIMESTAMPTZ
);
CREATE TABLE extraction_queue (
    id VARCHAR(36) PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id VARCHAR(36) NOT NULL,
    memory_uuid VARCHAR(36) NOT NULL
);
CREATE TABLE agent_memories_scope_repair_log (
    id BIGSERIAL PRIMARY KEY,
    memory_id VARCHAR(36) NOT NULL,
    kb_id VARCHAR(36) NOT NULL,
    old_tenant_id VARCHAR(36) NOT NULL,
    new_tenant_id VARCHAR(36) NOT NULL,
    old_fingerprint VARCHAR(64),
    new_fingerprint VARCHAR(64),
    repaired_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

func seedMemory(t *testing.T, db *gorm.DB, id, tenant, kb, fp string) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO agent_memories (id, tenant_id, kb_id, content, fingerprint) VALUES (?, ?, ?, ?, NULLIF(?, ''))`,
		id, tenant, kb, "content "+id, fp).Error)
}

func TestKBScopeRepairMovesMismatchedRowsToOwnerTenant(t *testing.T) {
	db := memoryV2PostgresDB(t)
	require.NoError(t, db.Exec(kbScopeExtraDDL).Error)
	repo := NewMemoryRepository(db)
	ctx := context.Background()

	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES ('kb-shared', 1), ('kb-own', 2)`).Error)
	seedMemory(t, db, "m-ok", "1", "kb-shared", "fp-ok")        // already in owner tenant
	seedMemory(t, db, "m-bad", "2", "kb-shared", "fp-old")      // written by tenant 2 member
	seedMemory(t, db, "m-bad2", "2", "kb-shared", "fp-old2")    // second wrong row, linked to m-bad
	seedMemory(t, db, "m-own", "2", "kb-own", "fp-own")         // own KB, correct
	seedMemory(t, db, "m-orphan", "3", "kb-missing", "fp-orph") // KB gone: not repairable
	seedMemory(t, db, "m-clash", "1", "kb-shared", "fp-new")    // occupies the destination fingerprint
	require.NoError(t, db.Exec(`INSERT INTO extraction_queue (tenant_id, memory_uuid) VALUES ('2', 'm-bad')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO memory_relations (id, tenant_id, from_uuid, to_uuid, relation_type)
		VALUES ('r1', '2', 'm-bad', 'm-bad2', 'related'), ('r2', '2', 'm-bad', 'm-own', 'related')`).Error)

	found, err := repo.ListKBScopeMismatches(ctx, 0)
	require.NoError(t, err)
	var ids []string
	for _, m := range found {
		ids = append(ids, m.MemoryID)
		assert.Equal(t, "2", m.CurrentTenant)
		assert.Equal(t, "1", m.OwnerTenant)
	}
	assert.ElementsMatch(t, []string{"m-bad", "m-bad2"}, ids, "only rows of an existing KB under a foreign tenant")

	for _, m := range found {
		fp := "fp-new" // collides with m-clash for the first row, free for the second
		if m.MemoryID == "m-bad2" {
			fp = "fp-new2"
		}
		require.NoError(t, repo.MoveToKBOwnerTenant(ctx, m, fp))
	}
	retenanted, dropped, err := repo.SyncRelationTenants(ctx, ids)
	require.NoError(t, err)
	assert.EqualValues(t, 1, retenanted, "m-bad -> m-bad2 now lives wholly in tenant 1")
	assert.EqualValues(t, 1, dropped, "m-bad -> m-own spans tenants and cannot be traversed")

	var rows []struct {
		ID          string
		TenantID    string
		Fingerprint *string
	}
	require.NoError(t, db.Raw(`SELECT id, tenant_id, fingerprint FROM agent_memories WHERE id IN ('m-bad','m-bad2') ORDER BY id`).Scan(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "1", rows[0].TenantID)
	assert.Nil(t, rows[0].Fingerprint, "a colliding fingerprint is cleared instead of failing the move")
	assert.Equal(t, "1", rows[1].TenantID)
	require.NotNil(t, rows[1].Fingerprint)
	assert.Equal(t, "fp-new2", *rows[1].Fingerprint)

	var queueTenant string
	require.NoError(t, db.Raw(`SELECT tenant_id FROM extraction_queue WHERE memory_uuid = 'm-bad'`).Scan(&queueTenant).Error)
	assert.Equal(t, "1", queueTenant)
	var logged int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM agent_memories_scope_repair_log WHERE old_tenant_id = '2' AND new_tenant_id = '1'`).Scan(&logged).Error)
	assert.EqualValues(t, 2, logged, "every move is audited")

	again, err := repo.ListKBScopeMismatches(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, again, "repair is idempotent")
	require.NoError(t, repo.MoveToKBOwnerTenant(ctx, found[0], "x"), "a stale replay is a no-op")
}
