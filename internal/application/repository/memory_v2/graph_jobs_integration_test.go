package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGraphJobs_LeaseTakeoverRetryAndGenerationFence(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	c := context.Background()
	m := createPostgresMemory(t, db, "1", "kb", "persistent graph job target", types.VerdictNone, []float32{1, 0})
	require.NoError(t, repo.EnqueueGraphLink(c, m))
	first, err := repo.ClaimGraphLink(c, "worker-1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.EqualValues(t, 1, first.Generation)
	require.Equal(t, 1, first.Attempts)
	busy, err := repo.ClaimGraphLink(c, "worker-2", time.Minute)
	require.NoError(t, err)
	require.Nil(t, busy)
	require.NoError(t, db.Exec("UPDATE extraction_queue SET locked_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=?", first.ID).Error)
	restarted := NewMemoryRepository(db)
	second, err := restarted.ClaimGraphLink(c, "worker-2", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 2, second.Attempts)
	require.NoError(t, repo.FinishGraphLink(c, first, true))
	var status string
	require.NoError(t, db.Raw("SELECT status FROM extraction_queue WHERE id=?", first.ID).Scan(&status).Error)
	require.Equal(t, "processing", status, "expired owner cannot complete a new lease")
	require.NoError(t, repo.FinishGraphLink(c, second, false))
	delayed, err := repo.ClaimGraphLink(c, "worker-3", time.Minute)
	require.NoError(t, err)
	require.Nil(t, delayed, "failed attempts back off")
	require.NoError(t, db.Exec("UPDATE extraction_queue SET run_after=CURRENT_TIMESTAMP WHERE id=?", first.ID).Error)
	old, err := repo.ClaimGraphLink(c, "worker-3", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, old)
	require.NoError(t, repo.EnqueueGraphLink(c, m)) // new content/version supersedes the active generation
	require.NoError(t, repo.FinishGraphLink(c, old, true))
	fresh, err := repo.ClaimGraphLink(c, "worker-4", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, fresh)
	require.EqualValues(t, 2, fresh.Generation)
	require.Equal(t, 1, fresh.Attempts)
	require.NoError(t, repo.FinishGraphLink(c, fresh, true))
	require.NoError(t, db.Raw("SELECT status FROM extraction_queue WHERE id=?", first.ID).Scan(&status).Error)
	require.Equal(t, "done", status)
	require.NoError(t, repo.EnqueueGraphLink(c, m))
	last, err := repo.ClaimGraphLink(c, "crashed-final-attempt", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, last)
	require.NoError(t, db.Exec("UPDATE extraction_queue SET attempts=max_attempts,locked_until=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=?", last.ID).Error)
	exhausted, err := repo.ClaimGraphLink(c, "worker-5", time.Minute)
	require.NoError(t, err)
	require.Nil(t, exhausted)
	require.NoError(t, db.Raw("SELECT status FROM extraction_queue WHERE id=?", first.ID).Scan(&status).Error)
	require.Equal(t, "failed", status, "a final-attempt crash becomes terminal rather than stuck processing")
	require.NoError(t, db.Unscoped().Where("id=?", m.ID).Delete(&types.AgentMemory{}).Error)
	var remaining int64
	require.NoError(t, db.Table("extraction_queue").Count(&remaining).Error)
	require.Zero(t, remaining, "hard deletion cascades obsolete jobs")
}

func TestVersionedRelation_RejectsStaleDeletedAndCrossScopeEndpoints(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	c := context.Background()
	a := createPostgresMemory(t, db, "1", "kb", "source graph endpoint", types.VerdictNone, []float32{1, 0})
	b := createPostgresMemory(t, db, "1", "kb", "target graph endpoint", types.VerdictNone, []float32{1, 0})
	foreign := createPostgresMemory(t, db, "1", "other-kb", "different scope endpoint", types.VerdictNone, []float32{1, 0})
	relation := func(to string) *types.MemoryRelation {
		return &types.MemoryRelation{TenantID: "1", FromUUID: a.ID, ToUUID: to, RelationType: "related_to", Weight: 1}
	}
	created, err := repo.CreateRelationForVersions(c, relation(b.ID), a.UpdatedAt, b.UpdatedAt)
	require.NoError(t, err)
	require.True(t, created)
	created, err = repo.CreateRelationForVersions(c, relation(b.ID), a.UpdatedAt, b.UpdatedAt)
	require.NoError(t, err)
	require.False(t, created, "idempotent retry creates no duplicate edge")
	_, err = repo.CreateRelationForVersions(c, relation(foreign.ID), a.UpdatedAt, foreign.UpdatedAt)
	require.ErrorContains(t, err, "across knowledge bases")
	stale := b.UpdatedAt
	require.NoError(t, repo.UpdateFieldsIf(c, "1", b.ID, map[string]interface{}{"importance": 4}, &stale))
	_, err = repo.CreateRelationForVersions(c, relation(b.ID), a.UpdatedAt, stale)
	var conflict *types.MemoryV2ConflictError
	require.ErrorAs(t, err, &conflict)
	require.NoError(t, repo.Delete(c, "1", b.ID))
	_, err = repo.CreateRelationForVersions(c, relation(b.ID), a.UpdatedAt, stale)
	require.Error(t, err, "a queued job cannot recreate an edge to a deleted memory")
	relations, err := repo.GetRelations(c, a.ID, "1")
	require.NoError(t, err)
	require.Empty(t, relations)
}
