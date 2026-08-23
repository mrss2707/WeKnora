package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testDDL defines the agent_memories table for SQLite tests.
// Avoids AutoMigrate to stay explicit about the schema under test.
const agentMemoriesTestDDL = `
CREATE TABLE IF NOT EXISTS agent_memories (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    kb_id VARCHAR(36) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    memory_type VARCHAR(32) NOT NULL DEFAULT '',
    importance INTEGER NOT NULL DEFAULT 0,
    tier INTEGER NOT NULL DEFAULT 2,
    verdict VARCHAR(16) NOT NULL DEFAULT 'none',
    hub_score REAL NOT NULL DEFAULT 0,
    embedding TEXT NOT NULL DEFAULT '[0]',
    access_count INTEGER NOT NULL DEFAULT 0,
    session_id VARCHAR(36) NOT NULL DEFAULT '',
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    fingerprint VARCHAR(64),
    tags TEXT NOT NULL DEFAULT '{}',
    metadata TEXT NOT NULL DEFAULT '{}',
    last_accessed_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);
`

const dreamerStateTestDDL = `
CREATE TABLE IF NOT EXISTS dreamer_state (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL UNIQUE,
    last_run_at DATETIME,
    locked_by VARCHAR(64) NOT NULL DEFAULT '',
    locked_until DATETIME,
    stats TEXT DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

const memoryRelationsTestDDL = `
CREATE TABLE IF NOT EXISTS memory_relations (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    from_uuid VARCHAR(36) NOT NULL,
    to_uuid VARCHAR(36) NOT NULL,
    relation_type VARCHAR(64) NOT NULL DEFAULT '',
    weight REAL NOT NULL DEFAULT 1.0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_relations_unique
    ON memory_relations(from_uuid, to_uuid, relation_type)
    WHERE deleted_at IS NULL;
`

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupMemoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(agentMemoriesTestDDL).Error)
	require.NoError(t, db.Exec(dreamerStateTestDDL).Error)
	require.NoError(t, db.Exec(memoryRelationsTestDDL).Error)
	return db
}

func newTestRepo(t *testing.T) (*MemoryRepository, *gorm.DB) {
	t.Helper()
	db := setupMemoryTestDB(t)
	return NewMemoryRepository(db), db
}

func createTestMemory(t *testing.T, db *gorm.DB, tenantID, content string, verdict types.MemoryVerdict) *types.AgentMemory {
	t.Helper()
	mem := types.NewMemory(tenantID, content)
	mem.ID = uuid.New().String()
	mem.Verdict = verdict
	mem.Embedding = pgvector.NewVector([]float32{0.0})
	require.NoError(t, db.Create(mem).Error)
	return mem
}

func createTestRelation(t *testing.T, db *gorm.DB, tenantID, fromID, toID string, weight float64) {
	t.Helper()
	rel := &types.MemoryRelation{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		FromUUID:     fromID,
		ToUUID:       toID,
		RelationType: "related_to",
		Weight:       weight,
	}
	require.NoError(t, db.Create(rel).Error)
}

func intPtr(i int) *int { return &i }

// ---------------------------------------------------------------------------
// CRUD tests
// ---------------------------------------------------------------------------

func TestMemoryCRUD_CreateAndGetByID(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem := types.NewMemory("tenant-1", "test content")
	mem.Embedding = pgvector.NewVector([]float32{0.0})
	err := repo.Create(ctx, mem)
	require.NoError(t, err)
	assert.NotEmpty(t, mem.ID)

	// Get by ID
	got, err := repo.GetByID(ctx, "tenant-1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, mem.ID, got.ID)
	assert.Equal(t, "test content", got.Content)
	assert.Equal(t, types.VerdictNone, got.Verdict)

	// Tenant isolation: wrong tenant returns ErrRecordNotFound
	_, err = repo.GetByID(ctx, "tenant-2", mem.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// Verify DB count
	var count int64
	db.Model(&types.AgentMemory{}).Where("tenant_id = ?", "tenant-1").Count(&count)
	assert.EqualValues(t, 1, count)
}

func TestMemoryCRUD_Update(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem := createTestMemory(t, db, "tenant-1", "original content", types.VerdictNone)

	mem.Content = "updated content"
	mem.Importance = 3
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, "tenant-1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated content", got.Content)
	assert.Equal(t, 3, got.Importance)
}

func TestMemoryCRUD_Delete(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem := createTestMemory(t, db, "tenant-1", "to delete", types.VerdictNone)

	err := repo.Delete(ctx, "tenant-1", mem.ID)
	require.NoError(t, err)

	// Should be soft-deleted
	_, err = repo.GetByID(ctx, "tenant-1", mem.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// Verify it's still in DB with deleted_at set
	var count int64
	db.Unscoped().Model(&types.AgentMemory{}).Where("id = ?", mem.ID).Count(&count)
	assert.EqualValues(t, 1, count)
}

func TestMemoryCRUD_TenantIsolation(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "tenant-1", "tenant 1 data", types.VerdictNone)
	mem2 := createTestMemory(t, db, "tenant-2", "tenant 2 data", types.VerdictNone)

	// Read isolation
	_, err := repo.GetByID(ctx, "tenant-1", mem2.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = repo.GetByID(ctx, "tenant-2", mem1.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// Delete isolation
	err = repo.Delete(ctx, "tenant-1", mem2.ID)
	require.NoError(t, err) // no error but no rows affected

	var count int64
	db.Model(&types.AgentMemory{}).Where("id = ?", mem2.ID).Count(&count)
	assert.EqualValues(t, 1, count) // still exists because tenant-1 can't delete tenant-2's mem
}

// ---------------------------------------------------------------------------
// Search / Verdict filtering tests
// ---------------------------------------------------------------------------

func TestSearch_DefaultExcludesRefuted(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "normal memory", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "refuted memory", types.VerdictRefuted)
	_ = createTestMemory(t, db, "t1", "fixed memory", types.VerdictFixed)

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID: "t1",
		Limit:    100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "refuted should be excluded by default")

	for _, r := range got {
		assert.NotEqual(t, types.VerdictRefuted, r.Memory.Verdict,
			"no refuted memory should appear in default search results")
	}
}

func TestSearch_ExplicitVerdictFilterIncludesRefuted(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "normal memory", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "refuted memory", types.VerdictRefuted)
	_ = createTestMemory(t, db, "t1", "fixed memory", types.VerdictFixed)

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID: "t1",
		Verdicts: []types.MemoryVerdict{types.VerdictNone, types.VerdictRefuted, types.VerdictFixed},
		Limit:    100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total, "all three verdicts should appear when explicitly requested")
	assert.Len(t, got, 3, "all three memories should be in results")
}

func TestSearch_FilterBySingleVerdict(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "normal", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "refuted", types.VerdictRefuted)
	_ = createTestMemory(t, db, "t1", "fixed", types.VerdictFixed)

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID: "t1",
		Verdicts: []types.MemoryVerdict{types.VerdictFixed},
		Limit:    100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, types.VerdictFixed, got[0].Memory.Verdict)
}

func TestSearch_FilterByMemoryType(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "episodic memory", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "semantic memory", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "procedural memory", types.VerdictNone)

	// Update memory_type after create (default is "semantic")
	db.Model(&types.AgentMemory{}).Where("content = ?", "episodic memory").Update("memory_type", "episodic")
	db.Model(&types.AgentMemory{}).Where("content = ?", "semantic memory").Update("memory_type", "semantic")
	db.Model(&types.AgentMemory{}).Where("content = ?", "procedural memory").Update("memory_type", "procedural")

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID:   "t1",
		MemoryType: "semantic",
		Limit:      100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "semantic", got[0].Memory.MemoryType)
}

func TestSearch_TextQuery(t *testing.T) {
	t.Skip("requires PostgreSQL full-text search (tsvector)")
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "the quick brown fox", types.VerdictNone)
	_ = createTestMemory(t, db, "t1", "lazy dog sleeps", types.VerdictNone)

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID: "t1",
		Query:    "fox",
		Limit:    100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Contains(t, got[0].Memory.Content, "fox")
}

func TestSearch_TenantIsolation(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	_ = createTestMemory(t, db, "t1", "tenant 1 data", types.VerdictNone)
	_ = createTestMemory(t, db, "t2", "tenant 2 data", types.VerdictNone)

	got1, total1, err := repo.Search(ctx, &types.MemoryFilter{TenantID: "t1", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total1)
	assert.Equal(t, "tenant 1 data", got1[0].Memory.Content)

	got2, total2, err := repo.Search(ctx, &types.MemoryFilter{TenantID: "t2", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total2)
	assert.Equal(t, "tenant 2 data", got2[0].Memory.Content)
}

func TestSearch_EmptyResults(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	got, total, err := repo.Search(ctx, &types.MemoryFilter{TenantID: "nonexistent", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, got)
}

func TestSearch_Pagination(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		createTestMemory(t, db, "t1", "memory "+uuid.New().String(), types.VerdictNone)
	}

	// Page 1
	got, total, err := repo.Search(ctx, &types.MemoryFilter{TenantID: "t1", Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Len(t, got, 2)

	// Page 2
	got, total, err = repo.Search(ctx, &types.MemoryFilter{TenantID: "t1", Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Len(t, got, 2)

	// Last page
	got, total, err = repo.Search(ctx, &types.MemoryFilter{TenantID: "t1", Limit: 2, Offset: 4})
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Len(t, got, 1)
}

// ---------------------------------------------------------------------------
// Protected verdict tests
// ---------------------------------------------------------------------------

func TestUpdate_ProtectedVerdictRejectedForDreamer(t *testing.T) {
	repo, db := newTestRepo(t)
	// Set actor = "dreamer" in context
	ctx := context.WithValue(context.Background(), types.ActorKey{}, "dreamer")

	mem := createTestMemory(t, db, "t1", "decision memory", types.VerdictDecision)

	mem.Content = "updated by dreamer"
	err := repo.Update(ctx, mem)
	require.Error(t, err)
	var protectedErr *types.ErrProtectedVerdict
	assert.True(t, errors.As(err, &protectedErr), "expected ErrProtectedVerdict")
}

func TestUpdate_ProtectedVerdictRejectedForSystem(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.WithValue(context.Background(), types.ActorKey{}, "system")

	mem := createTestMemory(t, db, "t1", "fixed memory", types.VerdictFixed)

	mem.Content = "updated by system"
	err := repo.Update(ctx, mem)
	require.Error(t, err)
	var protectedErr *types.ErrProtectedVerdict
	assert.True(t, errors.As(err, &protectedErr), "expected ErrProtectedVerdict")
}

func TestUpdate_ProtectedVerdictAllowedForHuman(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.WithValue(context.Background(), types.ActorKey{}, "human")

	mem := createTestMemory(t, db, "t1", "decision memory", types.VerdictDecision)

	mem.Content = "updated by human"
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, "t1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated by human", got.Content)
	assert.Equal(t, types.VerdictDecision, got.Verdict)
}

func TestUpdate_ProtectedVerdictAllowedWhenNoActorInContext(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background() // no actor key

	mem := createTestMemory(t, db, "t1", "fixed memory", types.VerdictFixed)

	mem.Content = "updated without actor"
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, "t1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated without actor", got.Content)
}

func TestUpdate_DreamerCanChangeNonProtectedVerdict(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.WithValue(context.Background(), types.ActorKey{}, "dreamer")

	mem := createTestMemory(t, db, "t1", "normal memory", types.VerdictNone)

	mem.Verdict = types.VerdictRefuted
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	got, _ := repo.GetByID(ctx, "t1", mem.ID)
	assert.Equal(t, types.VerdictRefuted, got.Verdict)
}

func TestUpdate_DreamerCanChangeProtectedToNonProtected(t *testing.T) {
	// Verdict guard checks the EXISTING verdict, not the new one.
	// Once a memory has a non-protected verdict, dreamer can update freely.
	repo, db := newTestRepo(t)
	ctx := context.WithValue(context.Background(), types.ActorKey{}, "dreamer")

	mem := createTestMemory(t, db, "t1", "was refuted", types.VerdictRefuted)

	mem.Verdict = types.VerdictNone
	mem.Content = "now normal"
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	got, _ := repo.GetByID(ctx, "t1", mem.ID)
	assert.Equal(t, types.VerdictNone, got.Verdict)
}

// ---------------------------------------------------------------------------
// Dreamer lock tests
// ---------------------------------------------------------------------------

func TestDreamerLock_AcquireNew(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	acquired, err := repo.TryDreamerLock(ctx, "tenant-1", "worker-1")
	require.NoError(t, err)
	assert.True(t, acquired, "should acquire lock when none exists")
}

func TestDreamerLock_ConflictWhenHeld(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	acquired, err := repo.TryDreamerLock(ctx, "tenant-1", "worker-1")
	require.NoError(t, err)
	assert.True(t, acquired)

	// Second worker tries to acquire the same lock
	acquired, err = repo.TryDreamerLock(ctx, "tenant-1", "worker-2")
	require.NoError(t, err)
	assert.False(t, acquired, "should NOT acquire lock when still held")
}

func TestDreamerLock_Release(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	acquired, err := repo.TryDreamerLock(ctx, "tenant-1", "worker-1")
	require.NoError(t, err)
	assert.True(t, acquired)

	// Release
	err = repo.UnlockDreamer(ctx, "tenant-1")
	require.NoError(t, err)

	// Now another worker can acquire
	acquired, err = repo.TryDreamerLock(ctx, "tenant-1", "worker-2")
	require.NoError(t, err)
	assert.True(t, acquired, "should acquire lock after release")
}

func TestDreamerLock_MultipleTenantsIndependent(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	acq1, _ := repo.TryDreamerLock(ctx, "tenant-1", "worker-1")
	assert.True(t, acq1)

	acq2, _ := repo.TryDreamerLock(ctx, "tenant-2", "worker-2")
	assert.True(t, acq2, "different tenants should not interfere")

	// Both still held
	acq1b, _ := repo.TryDreamerLock(ctx, "tenant-1", "worker-3")
	assert.False(t, acq1b)

	acq2b, _ := repo.TryDreamerLock(ctx, "tenant-2", "worker-4")
	assert.False(t, acq2b)

	// Release one, verify other still locked
	_ = repo.UnlockDreamer(ctx, "tenant-1")
	acq1c, _ := repo.TryDreamerLock(ctx, "tenant-1", "worker-3")
	assert.True(t, acq1c)

	acq2c, _ := repo.TryDreamerLock(ctx, "tenant-2", "worker-4")
	assert.False(t, acq2c, "tenant-2 lock should still be held")
}

// ---------------------------------------------------------------------------
// Hub score tests
// ---------------------------------------------------------------------------

func TestComputeHubScores_Basic(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "t1", "memory a", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "memory b", types.VerdictNone)
	mem3 := createTestMemory(t, db, "t1", "memory c", types.VerdictNone)

	// Set up relations: mem1 -> mem2 (weight 1.0), mem1 -> mem3 (weight 2.0)
	// mem1 has out-degree 2, avg weight 1.5
	// mem2 has in-degree 1, avg weight 1.0
	// mem3 has in-degree 1, avg weight 2.0
	createTestRelation(t, db, "t1", mem1.ID, mem2.ID, 1.0)
	createTestRelation(t, db, "t1", mem1.ID, mem3.ID, 2.0)

	err := repo.ComputeHubScores(ctx, "t1")
	require.NoError(t, err)

	// mem1: LN(1+2) * 1.5 = LN(3) * 1.5
	expectedHub1 := types.HubScoreFromDegree(2, 1.5)
	// mem2: LN(1+1) * 1.0 = LN(2) * 1.0
	expectedHub2 := types.HubScoreFromDegree(1, 1.0)
	// mem3: LN(1+1) * 2.0 = LN(2) * 2.0
	expectedHub3 := types.HubScoreFromDegree(1, 2.0)

	got1, _ := repo.GetByID(ctx, "t1", mem1.ID)
	assert.InDelta(t, expectedHub1, got1.HubScore, 0.0001, "mem1 hub_score")

	got2, _ := repo.GetByID(ctx, "t1", mem2.ID)
	assert.InDelta(t, expectedHub2, got2.HubScore, 0.0001, "mem2 hub_score")

	got3, _ := repo.GetByID(ctx, "t1", mem3.ID)
	assert.InDelta(t, expectedHub3, got3.HubScore, 0.0001, "mem3 hub_score")
}

func TestComputeHubScores_IsolatedByTenant(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	memA := createTestMemory(t, db, "t1", "a", types.VerdictNone)
	memB := createTestMemory(t, db, "t2", "b", types.VerdictNone)

	// Relation in t1 only
	createTestRelation(t, db, "t1", memA.ID, memA.ID, 1.0)

	err := repo.ComputeHubScores(ctx, "t1")
	require.NoError(t, err)

	gotA, _ := repo.GetByID(ctx, "t1", memA.ID)
	assert.NotZero(t, gotA.HubScore, "t1 memory should have hub_score > 0")

	gotB, _ := repo.GetByID(ctx, "t2", memB.ID)
	assert.Zero(t, gotB.HubScore, "t2 memory should have hub_score = 0")
}

func TestComputeHubScores_NoRelations(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem := createTestMemory(t, db, "t1", "lonely", types.VerdictNone)

	err := repo.ComputeHubScores(ctx, "t1")
	require.NoError(t, err)

	got, _ := repo.GetByID(ctx, "t1", mem.ID)
	assert.Zero(t, got.HubScore, "memory with no relations should have hub_score = 0")
}

func TestComputeHubScores_SoftDeletedRelationsIgnored(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "t1", "a", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "b", types.VerdictNone)

	// Active relation
	createTestRelation(t, db, "t1", mem1.ID, mem2.ID, 1.0)
	// Soft-deleted relation
	rel := &types.MemoryRelation{
		ID:       uuid.New().String(),
		TenantID: "t1",
		FromUUID: mem2.ID,
		ToUUID:   mem1.ID,
		Weight:   5.0,
	}
	require.NoError(t, db.Create(rel).Error)
	require.NoError(t, db.Delete(rel).Error)

	err := repo.ComputeHubScores(ctx, "t1")
	require.NoError(t, err)

	got1, _ := repo.GetByID(ctx, "t1", mem1.ID)
	expectedHub1 := types.HubScoreFromDegree(1, 1.0)
	assert.InDelta(t, expectedHub1, got1.HubScore, 0.0001, "soft-deleted relations must be ignored")

	got2, _ := repo.GetByID(ctx, "t1", mem2.ID)
	expectedHub2 := types.HubScoreFromDegree(1, 1.0)
	assert.InDelta(t, expectedHub2, got2.HubScore, 0.0001)
}

// ---------------------------------------------------------------------------
// CosineSearch (requires PostgreSQL+pgvector; expected to fail on SQLite)
// ---------------------------------------------------------------------------

func TestCosineSearch_MethodExists(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	// CosineSearch requires pgvector (PostgreSQL-only). We just verify the
	// method can be called and returns a sensible error in non-pgvector env.
	_, err := repo.CosineSearch(ctx, &types.MemoryFilter{
		TenantID: "t1",
	}, []float32{0.1, 0.2}, 10)
	assert.Error(t, err, "CosineSearch should fail without pgvector extension")
}

// ---------------------------------------------------------------------------
// Cache invalidation (no-op stub)
// ---------------------------------------------------------------------------

func TestInvalidateResultCache_NoOp(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	// Should not panic
	repo.InvalidateResultCache(ctx, "tenant-1")
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestUpdate_NonExistentMemory(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	mem := types.NewMemory("t1", "never saved")
	mem.ID = uuid.New().String()
	mem.Embedding = pgvector.NewVector([]float32{0.0})
	err := repo.Update(ctx, mem)
	require.NoError(t, err)

	// Verify it was created via GORM Save (upsert for non-deleted records)
	got, err := repo.GetByID(ctx, "t1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, "never saved", got.Content)
}

func TestSearch_SessionIDFilter(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "t1", "session a memory", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "session b memory", types.VerdictNone)

	db.Model(&types.AgentMemory{}).Where("id = ?", mem1.ID).Update("session_id", "session-a")
	db.Model(&types.AgentMemory{}).Where("id = ?", mem2.ID).Update("session_id", "session-b")

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID:  "t1",
		SessionID: "session-a",
		Limit:     100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "session a memory", got[0].Memory.Content)
}

func TestSearch_TierFilter(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "t1", "critical memory", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "edge memory", types.VerdictNone)

	db.Model(&types.AgentMemory{}).Where("id = ?", mem1.ID).Update("tier", 0)
	db.Model(&types.AgentMemory{}).Where("id = ?", mem2.ID).Update("tier", 3)

	got, total, err := repo.Search(ctx, &types.MemoryFilter{
		TenantID: "t1",
		Tier:     intPtr(0),
		Limit:    100,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "critical memory", got[0].Memory.Content)
}

func TestRelations_CreateGetDelete(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	mem1 := createTestMemory(t, db, "t1", "one", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "two", types.VerdictNone)
	mem3 := createTestMemory(t, db, "t1", "three", types.VerdictNone)
	otherTenant := createTestMemory(t, db, "t2", "other", types.VerdictNone)

	relOut := &types.MemoryRelation{ID: uuid.New().String(), TenantID: "t1", FromUUID: mem1.ID, ToUUID: mem2.ID, RelationType: "supports", Weight: 1.0}
	relIn := &types.MemoryRelation{ID: uuid.New().String(), TenantID: "t1", FromUUID: mem3.ID, ToUUID: mem1.ID, RelationType: "contradicts", Weight: 2.0}
	otherRel := &types.MemoryRelation{ID: uuid.New().String(), TenantID: "t2", FromUUID: otherTenant.ID, ToUUID: mem1.ID, RelationType: "supports", Weight: 3.0}
	require.NoError(t, repo.CreateRelation(ctx, relOut))
	require.NoError(t, repo.CreateRelation(ctx, relIn))
	require.NoError(t, repo.CreateRelation(ctx, otherRel))

	rels, err := repo.GetRelations(ctx, mem1.ID, "t1")
	require.NoError(t, err)
	require.Len(t, rels, 2)
	ids := []string{rels[0].ID, rels[1].ID}
	assert.ElementsMatch(t, []string{relOut.ID, relIn.ID}, ids)

	require.NoError(t, repo.DeleteRelation(ctx, relOut.ID, "t1"))
	rels, err = repo.GetRelations(ctx, mem1.ID, "t1")
	require.NoError(t, err)
	require.Len(t, rels, 1)
	assert.Equal(t, relIn.ID, rels[0].ID)
}

func TestCreateRelation_AssignsID(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	mem1 := createTestMemory(t, db, "t1", "one", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "two", types.VerdictNone)
	rel := &types.MemoryRelation{TenantID: "t1", FromUUID: mem1.ID, ToUUID: mem2.ID, RelationType: "supports"}

	require.NoError(t, repo.CreateRelation(ctx, rel))

	assert.NotEmpty(t, rel.ID)
}

func TestCreateRelation_DuplicateIgnored(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	mem1 := createTestMemory(t, db, "t1", "one", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t1", "two", types.VerdictNone)
	rel1 := &types.MemoryRelation{ID: uuid.New().String(), TenantID: "t1", FromUUID: mem1.ID, ToUUID: mem2.ID, RelationType: "supports"}
	rel2 := &types.MemoryRelation{ID: uuid.New().String(), TenantID: "t1", FromUUID: mem1.ID, ToUUID: mem2.ID, RelationType: "supports"}

	require.NoError(t, repo.CreateRelation(ctx, rel1))
	require.NoError(t, repo.CreateRelation(ctx, rel2))

	rels, err := repo.GetRelations(ctx, mem1.ID, "t1")
	require.NoError(t, err)
	assert.Len(t, rels, 1)
	assert.Equal(t, rel1.ID, rels[0].ID)
}

func TestGetByFingerprint_FoundTenantScoped(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	fingerprint := "fp-shared"
	mem1 := createTestMemory(t, db, "t1", "tenant one", types.VerdictNone)
	mem2 := createTestMemory(t, db, "t2", "tenant two", types.VerdictNone)
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", mem1.ID).Update("fingerprint", fingerprint).Error)
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", mem2.ID).Update("fingerprint", fingerprint).Error)

	got, err := repo.GetByFingerprint(ctx, "t1", fingerprint)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, mem1.ID, got.ID)
}

func TestGetByFingerprint_NotFoundReturnsNilNil(t *testing.T) {
	repo, _ := newTestRepo(t)
	got, err := repo.GetByFingerprint(context.Background(), "t1", "missing")

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetByFingerprint_IgnoresSoftDeleted(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	fingerprint := "fp-deleted"
	mem := createTestMemory(t, db, "t1", "deleted", types.VerdictNone)
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", mem.ID).Update("fingerprint", fingerprint).Error)
	require.NoError(t, repo.Delete(ctx, "t1", mem.ID))

	got, err := repo.GetByFingerprint(ctx, "t1", fingerprint)

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestHardDeleteExpired(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	cutoff := now.Add(-30 * 24 * time.Hour)

	active := createTestMemory(t, db, "t1", "active", types.VerdictNone)
	oldDeletedUnused := createTestMemory(t, db, "t1", "old deleted unused", types.VerdictNone)
	newDeleted := createTestMemory(t, db, "t1", "new deleted", types.VerdictNone)
	oldDeletedUsed := createTestMemory(t, db, "t1", "old deleted used", types.VerdictNone)
	wrongTenant := createTestMemory(t, db, "t2", "wrong tenant", types.VerdictNone)

	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", oldDeletedUsed.ID).Update("access_count", 1).Error)
	softDeleteAt(t, db, oldDeletedUnused.ID, cutoff.Add(-24*time.Hour))
	softDeleteAt(t, db, newDeleted.ID, cutoff.Add(24*time.Hour))
	softDeleteAt(t, db, oldDeletedUsed.ID, cutoff.Add(-24*time.Hour))
	softDeleteAt(t, db, wrongTenant.ID, cutoff.Add(-24*time.Hour))

	rows, err := repo.HardDeleteExpired(ctx, "t1", cutoff)

	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)
	assertMemoryRowExists(t, db, active.ID)
	assertMemoryRowMissing(t, db, oldDeletedUnused.ID)
	assertMemoryRowExists(t, db, newDeleted.ID)
	assertMemoryRowExists(t, db, oldDeletedUsed.ID)
	assertMemoryRowExists(t, db, wrongTenant.ID)
}

func softDeleteAt(t *testing.T, db *gorm.DB, id string, deletedAt time.Time) {
	t.Helper()
	require.NoError(t, db.Model(&types.AgentMemory{}).Unscoped().Where("id = ?", id).Update("deleted_at", deletedAt).Error)
}

func assertMemoryRowExists(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Unscoped().Model(&types.AgentMemory{}).Where("id = ?", id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func assertMemoryRowMissing(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Unscoped().Model(&types.AgentMemory{}).Where("id = ?", id).Count(&count).Error)
	assert.Zero(t, count)
}

type testCacheInvalidator struct {
	prefixes []string
}

func (c *testCacheInvalidator) InvalidateByPrefix(prefix string) {
	c.prefixes = append(c.prefixes, prefix)
}

func TestInvalidateResultCache_UsesTenantPrefix(t *testing.T) {
	repo, _ := newTestRepo(t)
	cache := &testCacheInvalidator{}
	repo.SetCacheInvalidator(cache)

	repo.InvalidateResultCache(context.Background(), "tenant-1")

	assert.Equal(t, []string{"mem:tenant-1"}, cache.prefixes)
}

func TestGetEmbeddingDimension_EmptyTableReturnsZero(t *testing.T) {
	repo, _ := newTestRepo(t)
	dim, err := repo.GetEmbeddingDimension(context.Background(), "t1")

	require.NoError(t, err)
	assert.Zero(t, dim)
}

func TestGetEmbeddingDimension_ReturnsStoredDimension(t *testing.T) {
	repo, db := newTestRepo(t)
	mem := createTestMemory(t, db, "t1", "embedded", types.VerdictNone)
	vec := pgvector.NewVector([]float32{0.1, 0.2, 0.3})
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", mem.ID).Update("embedding", vec).Error)

	dim, err := repo.GetEmbeddingDimension(context.Background(), "t1")

	require.NoError(t, err)
	assert.Equal(t, 3, dim)
}

func TestPadEmbedding_PadsToColumnWidth(t *testing.T) {
	padded, err := padEmbedding([]float32{0.1, 0.2, 0.3})
	require.NoError(t, err)
	assert.Len(t, padded, types.MemoryEmbeddingDim)
	assert.Equal(t, float32(0.1), padded[0])
	assert.Equal(t, float32(0.3), padded[2])
	assert.Equal(t, float32(0), padded[types.MemoryEmbeddingDim-1])
}

func TestPadEmbedding_EmptyBecomesZeroVector(t *testing.T) {
	padded, err := padEmbedding(nil)
	require.NoError(t, err)
	assert.Len(t, padded, types.MemoryEmbeddingDim)
	for i, v := range padded {
		assert.Zero(t, v, "dimension %d should be zero", i)
	}
}

func TestPadEmbedding_ExactWidthIsNoOp(t *testing.T) {
	full := make([]float32, types.MemoryEmbeddingDim)
	full[7] = 0.7
	padded, err := padEmbedding(full)
	require.NoError(t, err)
	assert.Equal(t, float32(0.7), padded[7])
}

func TestPadEmbedding_RejectsOverWidth(t *testing.T) {
	tooWide := make([]float32, types.MemoryEmbeddingDim+1)
	_, err := padEmbedding(tooWide)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the maximum supported dimension")
}

func TestCreate_PadsEmbeddingToColumnWidth(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	mem := types.NewMemory("tenant-1", "padded content")
	mem.Embedding = pgvector.NewVector([]float32{0.1, 0.2, 0.3})
	require.NoError(t, repo.Create(ctx, mem))
	assert.Len(t, mem.Embedding.Slice(), types.MemoryEmbeddingDim)
	assert.Equal(t, float32(0.3), mem.Embedding.Slice()[2])
	assert.Equal(t, float32(0), mem.Embedding.Slice()[types.MemoryEmbeddingDim-1])
}

func TestCreate_RejectsEmbeddingOverColumnWidth(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	mem := types.NewMemory("tenant-1", "too-wide content")
	mem.Embedding = pgvector.NewVector(make([]float32, types.MemoryEmbeddingDim+1))
	err := repo.Create(ctx, mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the maximum supported dimension")
}

func TestBM25Search_EmptyQueryReturnsNil(t *testing.T) {
	repo, _ := newTestRepo(t)
	results, err := repo.BM25Search(context.Background(), &types.MemoryFilter{TenantID: "t1"})

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestBM25Search_PostgresSpecificFallbackReturnsErrorOnSQLite(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	createTestMemory(t, db, "t1", "searchable content", types.VerdictNone)

	_, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "t1", Query: "searchable", Limit: 10})

	assert.Error(t, err, "BM25Search should fail on SQLite because it uses PostgreSQL full-text functions")
}
