package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func relate(t *testing.T, db *gorm.DB, from, to, kind string) {
	t.Helper()
	require.NoError(t, db.Create(&types.MemoryRelation{
		ID: uuid.New().String(), TenantID: "1", FromUUID: from, ToUUID: to, RelationType: kind, Weight: 0.8,
	}).Error)
}

type liveEdge struct{ From, To, Type string }

func liveEdges(t *testing.T, db *gorm.DB) map[liveEdge]bool {
	t.Helper()
	var rels []types.MemoryRelation
	require.NoError(t, db.Where("deleted_at IS NULL").Find(&rels).Error)
	out := map[liveEdge]bool{}
	for _, r := range rels {
		out[liveEdge{r.FromUUID, r.ToUUID, r.RelationType}] = true
	}
	return out
}

func TestPostgresMemoryRepository_MergeIntoRepointsRelationsAtomically(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	survivor := createPostgresMemory(t, db, "1", "kb", "survivor", types.VerdictNone, []float32{1, 0})
	loser := createPostgresMemory(t, db, "1", "kb", "loser", types.VerdictNone, []float32{0, 1})
	x := createPostgresMemory(t, db, "1", "kb", "neighbour x", types.VerdictNone, []float32{1, 1})
	y := createPostgresMemory(t, db, "1", "kb", "neighbour y", types.VerdictNone, []float32{1, 1})
	relate(t, db, loser.ID, x.ID, "related_to")    // moves to survivor -> x
	relate(t, db, y.ID, loser.ID, "justifies")     // moves to y -> survivor
	relate(t, db, survivor.ID, x.ID, "related_to") // survivor already has this: no duplicate
	relate(t, db, survivor.ID, loser.ID, "related_to")

	survivor.Content = "survivor\n---\nloser"
	survivor.Importance = 5
	err := repo.MergeInto(ctx, survivor, []string{loser.ID}, []*types.MemoryAuditEntry{
		{TenantID: "1", KbID: "kb", MemoryID: survivor.ID, Action: types.MemoryAuditMerged, Before: []byte(`{"content":"survivor"}`)},
		{TenantID: "1", KbID: "kb", MemoryID: loser.ID, Action: types.MemoryAuditMergedIn},
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, "1", survivor.ID)
	require.NoError(t, err)
	assert.Equal(t, "survivor\n---\nloser", got.Content)
	assert.Equal(t, 5, got.Importance)
	_, err = repo.GetByID(ctx, "1", loser.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "loser soft-deleted")

	edges := liveEdges(t, db)
	assert.Equal(t, map[liveEdge]bool{
		{survivor.ID, x.ID, "related_to"}: true,
		{y.ID, survivor.ID, "justifies"}:  true,
	}, edges, "edges re-pointed once, no self-loop, none left on the loser")

	entries, total, err := repo.ListAudit(ctx, types.MemoryAuditFilter{TenantID: "1", KbID: "kb"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, entries, 2)

	// Undo: survivor text restored, loser undeleted.
	before := *got
	before.Content = "survivor"
	before.Importance = 1
	require.NoError(t, repo.RestoreMerge(ctx, &before, []*types.AgentMemory{{ID: loser.ID}}, nil))
	got, err = repo.GetByID(ctx, "1", survivor.ID)
	require.NoError(t, err)
	assert.Equal(t, "survivor", got.Content)
	revived, err := repo.GetByID(ctx, "1", loser.ID)
	require.NoError(t, err)
	assert.Equal(t, "loser", revived.Content)
}

func TestPostgresMemoryRepository_UpdateFieldsIfOptimisticLock(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	m := createPostgresMemory(t, db, "1", "kb", "versioned memory", types.VerdictNone, []float32{1, 0})
	read, err := repo.GetByID(ctx, "1", m.ID)
	require.NoError(t, err)
	version := read.UpdatedAt

	require.NoError(t, repo.UpdateFieldsIf(ctx, "1", m.ID, map[string]interface{}{"importance": 3}, &version))

	err = repo.UpdateFieldsIf(ctx, "1", m.ID, map[string]interface{}{"importance": 4}, &version)
	var conflict *types.MemoryV2ConflictError
	require.ErrorAs(t, err, &conflict, "second writer with the stale version loses")
	got, err := repo.GetByID(ctx, "1", m.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Importance)

	stale := time.Now().Add(-time.Hour)
	err = repo.UpdateFieldsIf(ctx, "1", "missing-id", map[string]interface{}{"importance": 1}, &stale)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "missing rows are not conflicts")
}

func TestPostgresMemoryRepository_UpdateFieldsDuplicateFingerprintIsConflict(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	a := createPostgresMemory(t, db, "1", "kb", "a", types.VerdictNone, []float32{1, 0})
	b := createPostgresMemory(t, db, "1", "kb", "b", types.VerdictNone, []float32{0, 1})
	require.NoError(t, repo.UpdateFields(context.Background(), "1", a.ID, map[string]interface{}{"fingerprint": "fp-1"}))

	err := repo.UpdateFields(context.Background(), "1", b.ID, map[string]interface{}{"fingerprint": "fp-1"})
	var conflict *types.MemoryV2ConflictError
	assert.ErrorAs(t, err, &conflict)
}

func TestPostgresMemoryRepository_DeleteSoftDeletesRelations(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	a := createPostgresMemory(t, db, "1", "kb", "a", types.VerdictNone, []float32{1, 0})
	b := createPostgresMemory(t, db, "1", "kb", "b", types.VerdictNone, []float32{0, 1})
	c := createPostgresMemory(t, db, "1", "kb", "c", types.VerdictNone, []float32{1, 1})
	relate(t, db, a.ID, b.ID, "related_to")
	relate(t, db, c.ID, a.ID, "justifies")
	relate(t, db, b.ID, c.ID, "related_to")

	require.NoError(t, repo.Delete(context.Background(), "1", a.ID))

	assert.Equal(t, map[liveEdge]bool{{b.ID, c.ID, "related_to"}: true}, liveEdges(t, db))
}

func TestPostgresMemoryRepository_Stats(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	createPostgresMemory(t, db, "1", "kb", "one", types.VerdictNone, []float32{1, 0})
	createPostgresMemory(t, db, "1", "kb", "two", types.VerdictDecision, []float32{1, 0})
	createPostgresMemory(t, db, "1", "other", "three", types.VerdictNone, []float32{1, 0})

	st, err := repo.Stats(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.EqualValues(t, 2, st.TotalMemories)
	assert.Equal(t, map[string]int64{"semantic": 2}, st.ByType)
	assert.Equal(t, map[string]int64{"2": 2}, st.ByTier)
	assert.Equal(t, map[string]int64{"none": 1, "decision": 1}, st.ByVerdict)
	assert.InDelta(t, 1.0, st.AvgImportance, 1e-9)

	all, err := repo.Stats(context.Background(), "1", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, all.TotalMemories)
}
