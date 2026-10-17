package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAtomicMutation_AuditFailureRollsBackPatchAndDelete(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	c := context.Background()
	a := createPostgresMemory(t, db, "1", "kb", "audited survivor", types.VerdictNone, []float32{1, 0})
	b := createPostgresMemory(t, db, "1", "kb", "related neighbour", types.VerdictNone, []float32{1, 0})
	require.NoError(t, repo.CreateRelation(c, &types.MemoryRelation{ID: uuid.NewString(), TenantID: "1", FromUUID: a.ID, ToUUID: b.ID, RelationType: "related_to", Weight: 1}))
	build := func(before, after *types.AgentMemory) *types.MemoryAuditEntry {
		previous, _ := json.Marshal(before)
		next, _ := json.Marshal(after)
		return &types.MemoryAuditEntry{TenantID: "1", KbID: "kb", MemoryID: a.ID, Actor: "user", Action: types.MemoryAuditPatched, Before: previous, After: next}
	}
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$;
 CREATE TRIGGER reject_test_audit BEFORE INSERT ON agent_memories_audit FOR EACH ROW EXECUTE FUNCTION reject_test_audit();`).Error)
	candidate := *a
	candidate.ID, candidate.Content = uuid.NewString(), "creation must roll back with its audit"
	err := repo.CreateAndAudit(c, &candidate, func(_, after *types.AgentMemory) *types.MemoryAuditEntry {
		return &types.MemoryAuditEntry{TenantID: "1", KbID: "kb", MemoryID: after.ID, Actor: "user", Action: types.MemoryAuditCreated}
	})
	require.ErrorContains(t, err, "audit unavailable")
	_, err = repo.GetByID(c, "1", candidate.ID)
	require.Error(t, err, "failed creation must leave no memory row")
	_, err = repo.UpdateFieldsAndAudit(c, "1", a.ID, map[string]interface{}{"importance": 5}, &a.UpdatedAt, build)
	require.ErrorContains(t, err, "audit unavailable")
	got, err := repo.GetByID(c, "1", a.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.Importance, "audit failure must undo the update")
	err = repo.DeleteAndAudit(c, "1", a.ID, build)
	require.ErrorContains(t, err, "audit unavailable")
	_, err = repo.GetByID(c, "1", a.ID)
	require.NoError(t, err, "audit failure must undo the delete")
	relations, err := repo.GetRelations(c, a.ID, "1")
	require.NoError(t, err)
	require.Len(t, relations, 1, "relation deletion rolls back with the memory")
	require.NoError(t, db.Exec("DROP TRIGGER reject_test_audit ON agent_memories_audit").Error)
	after, err := repo.UpdateFieldsAndAudit(c, "1", a.ID, map[string]interface{}{"importance": 5}, &got.UpdatedAt, build)
	require.NoError(t, err)
	require.Equal(t, 5, after.Importance)
	require.NoError(t, repo.DeleteAndAudit(c, "1", a.ID, build))
	var count int64
	require.NoError(t, db.Model(&types.MemoryAuditEntry{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}
