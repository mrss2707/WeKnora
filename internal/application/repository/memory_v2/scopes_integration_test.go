package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresMemoryRepository_ListMemoryScopes(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	createPostgresMemory(t, db, "2", "kb-b", "tenant two memory", types.VerdictNone, []float32{1, 0})
	createPostgresMemory(t, db, "1", "kb-a", "tenant one memory", types.VerdictNone, []float32{1, 0})
	createPostgresMemory(t, db, "1", "kb-a", "another tenant one memory", types.VerdictNone, []float32{0, 1})
	gone := createPostgresMemory(t, db, "3", "kb-c", "deleted memory", types.VerdictNone, []float32{1, 0})
	require.NoError(t, repo.Delete(context.Background(), "3", gone.ID))

	scopes, err := repo.ListMemoryScopes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []types.MemoryScope{{TenantID: "1", KbID: "kb-a"}, {TenantID: "2", KbID: "kb-b"}}, scopes,
		"distinct live scopes only, stable order")
}

func TestPostgresMemoryRepository_CosineSearchFiltersEmbeddingModel(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	modelA := createPostgresMemory(t, db, "1", "kb", "embedded by model A", types.VerdictNone, []float32{1, 0})
	modelB := createPostgresMemory(t, db, "1", "kb", "embedded by model B", types.VerdictNone, []float32{1, 0})
	legacy := createPostgresMemory(t, db, "1", "kb", "legacy row without model", types.VerdictNone, []float32{1, 0})
	require.NoError(t, db.Exec(`UPDATE agent_memories SET metadata = '{"embedding_model_id":"A"}' WHERE id = ?`, modelA.ID).Error)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET metadata = '{"embedding_model_id":"B"}' WHERE id = ?`, modelB.ID).Error)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET metadata = NULL WHERE id = ?`, legacy.ID).Error)

	results, err := repo.CosineSearch(context.Background(), &types.MemoryFilter{TenantID: "1", KbID: "kb", EmbeddingModelID: "A"}, []float32{1, 0}, 10)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.Memory.ID] = true
	}
	assert.True(t, ids[modelA.ID], "same model kept")
	assert.False(t, ids[legacy.ID], "unknown legacy vectors require re-embedding before comparison")
	assert.False(t, ids[modelB.ID], "vectors of another model are never compared")

	all, err := repo.CosineSearch(context.Background(), &types.MemoryFilter{TenantID: "1", KbID: "kb"}, []float32{1, 0}, 10)
	require.NoError(t, err)
	assert.Len(t, all, 3, "no model filter when the query model is unknown")
}
