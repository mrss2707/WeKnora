package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func TestGraphJobEnqueueFailure_RollsBackMemoryAndAudit(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	c := context.Background()
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_test_graph_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'graph queue unavailable'; END $$;
 CREATE TRIGGER reject_test_graph_job BEFORE INSERT ON extraction_queue FOR EACH ROW EXECUTE FUNCTION reject_test_graph_job();`).Error)
	m := &types.AgentMemory{TenantID: "1", KbID: "kb", Content: "atomic graph queue memory", Verdict: types.VerdictNone, Embedding: pgvector.NewVector([]float32{1, 0}), Metadata: []byte(`{"embedding_model_id":"model"}`)}
	audit := func(_, after *types.AgentMemory) *types.MemoryAuditEntry {
		return &types.MemoryAuditEntry{TenantID: "1", KbID: "kb", MemoryID: after.ID, Actor: "user", Action: types.MemoryAuditCreated}
	}
	err := repo.CreateAndAudit(c, m, audit)
	require.ErrorContains(t, err, "graph queue unavailable")
	_, err = repo.GetByID(c, "1", m.ID)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.MemoryAuditEntry{}).Count(&count).Error)
	require.Zero(t, count, "no committed audit for rolled-back creation")
	require.NoError(t, db.Exec("ALTER TABLE extraction_queue DISABLE TRIGGER reject_test_graph_job").Error)
	require.NoError(t, repo.CreateAndAudit(c, m, audit))
	require.NoError(t, db.Exec("ALTER TABLE extraction_queue ENABLE TRIGGER reject_test_graph_job").Error)
	_, err = repo.UpdateFieldsAndAudit(c, "1", m.ID, map[string]interface{}{"content": "must not survive a queue failure"}, &m.UpdatedAt, func(before, after *types.AgentMemory) *types.MemoryAuditEntry {
		return &types.MemoryAuditEntry{TenantID: "1", KbID: "kb", MemoryID: before.ID, Actor: "user", Action: types.MemoryAuditPatched}
	})
	require.ErrorContains(t, err, "graph queue unavailable")
	current, err := repo.GetByID(c, "1", m.ID)
	require.NoError(t, err)
	require.Equal(t, "atomic graph queue memory", current.Content)
	require.NoError(t, db.Model(&types.MemoryAuditEntry{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "failed queue update rolls audit back too")
	var generation int64
	require.NoError(t, db.Raw("SELECT generation FROM extraction_queue WHERE memory_uuid=?", m.ID).Scan(&generation).Error)
	require.EqualValues(t, 1, generation)
}
