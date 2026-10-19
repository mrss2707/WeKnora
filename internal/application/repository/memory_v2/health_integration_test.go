package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestHealthEmbeddingDimensionIsScoped(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	memory := createPostgresMemory(t, db, "1", "kb", "scoped dimension target", types.VerdictNone, []float32{1, 0})
	createPostgresMemory(t, db, "1", "other", "other KB vector", types.VerdictNone, []float32{0, 1})
	createPostgresMemory(t, db, "2", "kb", "other tenant vector", types.VerdictNone, []float32{0, 1})
	ctx := context.Background()
	dimension, err := repo.GetHealthEmbeddingDimension(ctx, "1", "kb")
	require.NoError(t, err)
	require.Equal(t, types.MemoryEmbeddingDim, dimension)
	require.NoError(t, db.Delete(memory).Error)
	dimension, err = repo.GetHealthEmbeddingDimension(ctx, "1", "kb")
	require.NoError(t, err)
	require.Zero(t, dimension, "other KBs and tenants must not provide the dimension sample")
}
