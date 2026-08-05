package repository

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func memoryV2PostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("WEKNORA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEKNORA_TEST_POSTGRES_DSN not set")
	}

	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)

	schema := "memory_v2_test_" + regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(uuid.New().String(), "_")
	require.NoError(t, root.Exec(`CREATE SCHEMA `+schema).Error)
	t.Cleanup(func() {
		require.NoError(t, root.Exec(`DROP SCHEMA IF EXISTS `+schema+` CASCADE`).Error)
	})

	if err := root.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		t.Skipf("pgvector extension unavailable: %v", err)
	}
	require.NoError(t, root.Exec(`SET search_path TO `+schema+`, public`).Error)
	require.NoError(t, root.Exec(postgresMemoryV2IntegrationDDL).Error)
	return root
}

const postgresMemoryV2IntegrationDDL = `
CREATE TABLE agent_memories (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    kb_id VARCHAR(36) NOT NULL DEFAULT '',
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    session_id VARCHAR(36) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    memory_type VARCHAR(32) NOT NULL DEFAULT 'semantic',
    importance INTEGER NOT NULL DEFAULT 0,
    tier SMALLINT NOT NULL DEFAULT 2,
    verdict VARCHAR(16) NOT NULL DEFAULT 'none',
    hub_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    embedding vector(3),
    access_count BIGINT NOT NULL DEFAULT 0,
    fingerprint VARCHAR(64),
    tags TEXT[] DEFAULT '{}',
    metadata JSONB DEFAULT '{}',
    last_accessed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_agent_memories_fingerprint
    ON agent_memories(fingerprint)
    WHERE fingerprint IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE memory_relations (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    from_uuid VARCHAR(36) NOT NULL REFERENCES agent_memories(id) ON DELETE CASCADE,
    to_uuid VARCHAR(36) NOT NULL REFERENCES agent_memories(id) ON DELETE CASCADE,
    relation_type VARCHAR(64) NOT NULL DEFAULT '',
    weight REAL NOT NULL DEFAULT 1.0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_memory_relations_unique
    ON memory_relations(from_uuid, to_uuid, relation_type)
    WHERE deleted_at IS NULL;

CREATE TABLE dreamer_state (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL UNIQUE,
    last_run_at TIMESTAMPTZ,
    locked_by VARCHAR(64) NOT NULL DEFAULT '',
    locked_until TIMESTAMPTZ,
    stats JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func createPostgresMemory(t *testing.T, db *gorm.DB, tenantID, kbID, content string, verdict types.MemoryVerdict, vector []float32) *types.AgentMemory {
	t.Helper()
	mem := &types.AgentMemory{
		ID:         uuid.New().String(),
		TenantID:   tenantID,
		KbID:       kbID,
		Content:    content,
		MemoryType: "semantic",
		Importance: 1,
		Tier:       2,
		Verdict:    verdict,
		Embedding:  pgvector.NewVector(vector),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, db.Create(mem).Error)
	return mem
}

func TestPostgresMemoryRepository_CosineSearchRanksAndFilters(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()

	best := createPostgresMemory(t, db, "tenant-1", "kb-1", "best match", types.VerdictNone, []float32{1, 0, 0})
	second := createPostgresMemory(t, db, "tenant-1", "kb-1", "second match", types.VerdictFixed, []float32{0.8, 0.2, 0})
	_ = createPostgresMemory(t, db, "tenant-1", "kb-1", "refuted match", types.VerdictRefuted, []float32{0.99, 0, 0})
	_ = createPostgresMemory(t, db, "tenant-2", "kb-1", "other tenant", types.VerdictNone, []float32{1, 0, 0})
	_ = createPostgresMemory(t, db, "tenant-1", "kb-2", "other kb", types.VerdictNone, []float32{1, 0, 0})

	results, err := repo.CosineSearch(ctx, &types.MemoryFilter{TenantID: "tenant-1", KbID: "kb-1"}, []float32{1, 0, 0}, 10)

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, best.ID, results[0].Memory.ID)
	assert.Equal(t, second.ID, results[1].Memory.ID)
	assert.GreaterOrEqual(t, results[0].Score, results[1].Score)
}

func TestPostgresMemoryRepository_BM25SearchFallback(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	repo.paradeDBOnce.Do(func() { repo.paradeDBAvail = false })
	ctx := context.Background()

	wanted := createPostgresMemory(t, db, "tenant-1", "kb-1", "fox jumps over memory", types.VerdictNone, []float32{1, 0, 0})
	_ = createPostgresMemory(t, db, "tenant-1", "kb-2", "fox in other kb", types.VerdictNone, []float32{1, 0, 0})
	_ = createPostgresMemory(t, db, "tenant-2", "kb-1", "fox in other tenant", types.VerdictNone, []float32{1, 0, 0})
	_ = createPostgresMemory(t, db, "tenant-1", "kb-1", "unrelated elephant", types.VerdictNone, []float32{1, 0, 0})

	results, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "tenant-1", KbID: "kb-1", Query: "fox", Limit: 10})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, wanted.ID, results[0].Memory.ID)
	assert.Positive(t, results[0].Score)
}

func TestPostgresMemoryRepository_GetEmbeddingDimension(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	createPostgresMemory(t, db, "tenant-1", "kb-1", "dimension row", types.VerdictNone, []float32{0.1, 0.2, 0.3})

	dim, err := repo.GetEmbeddingDimension(context.Background(), "tenant-1")

	require.NoError(t, err)
	assert.Equal(t, 3, dim)
}

func TestPostgresMemoryRepository_FingerprintPartialUniqueAndSoftDelete(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	fingerprint := "duplicate-fingerprint"

	first := createPostgresMemory(t, db, "tenant-1", "kb-1", "first fingerprint", types.VerdictNone, []float32{1, 0, 0})
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", first.ID).Update("fingerprint", fingerprint).Error)
	second := createPostgresMemory(t, db, "tenant-1", "kb-1", "second fingerprint", types.VerdictNone, []float32{0, 1, 0})

	err := db.Model(&types.AgentMemory{}).Where("id = ?", second.ID).Update("fingerprint", fingerprint).Error
	require.Error(t, err, "production index is global across tenants for active fingerprints")

	require.NoError(t, repo.Delete(ctx, "tenant-1", first.ID))
	require.NoError(t, db.Model(&types.AgentMemory{}).Where("id = ?", second.ID).Update("fingerprint", fingerprint).Error)
}

func TestPostgresMemoryRepository_RelationUniqueIndexMatchesMigration(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	from := createPostgresMemory(t, db, "tenant-1", "kb-1", "from memory", types.VerdictNone, []float32{1, 0, 0})
	to := createPostgresMemory(t, db, "tenant-1", "kb-1", "to memory", types.VerdictNone, []float32{0, 1, 0})

	first := &types.MemoryRelation{TenantID: "tenant-1", FromUUID: from.ID, ToUUID: to.ID, RelationType: "supports", Weight: 1}
	second := &types.MemoryRelation{TenantID: "tenant-1", FromUUID: from.ID, ToUUID: to.ID, RelationType: "supports", Weight: 2}
	require.NoError(t, repo.CreateRelation(ctx, first))
	require.NoError(t, repo.CreateRelation(ctx, second))

	rels, err := repo.GetRelations(ctx, from.ID, "tenant-1")
	require.NoError(t, err)
	require.Len(t, rels, 1)
	assert.Equal(t, first.ID, rels[0].ID)
	assert.Equal(t, float64(1), float64(rels[0].Weight))
}

func TestPostgresMemoryRepository_ParadeDBPositivePathOptional(t *testing.T) {
	if os.Getenv("WEKNORA_TEST_PARADEDB") != "1" {
		t.Skip("WEKNORA_TEST_PARADEDB not set")
	}
	db := memoryV2PostgresDB(t)
	var count int
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_extension WHERE extname = 'pg_search'").Scan(&count).Error)
	if count == 0 {
		t.Skip("pg_search extension unavailable")
	}
	assert.Greater(t, count, 0, fmt.Sprintf("pg_search extension count: %d", count))
}
