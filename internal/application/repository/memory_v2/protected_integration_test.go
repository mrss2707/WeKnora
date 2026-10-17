package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPostgresMemoryRepository_UpdateFieldsWritesOnlyNamedColumns(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	mem := createPostgresMemory(t, db, "t1", "kb1", "Kết nối postgres bị đầy pool", types.VerdictNone, []float32{1, 0})
	ctx := context.Background()

	require.NoError(t, repo.UpdateFields(ctx, "t1", mem.ID, map[string]interface{}{
		"verdict":    "gotcha",
		"importance": 0,
		"tags":       types.TagsArray{"postgres", "kết nối"},
	}))

	got, err := repo.GetByID(ctx, "t1", mem.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictGotcha, got.Verdict)
	assert.Equal(t, 0, got.Importance)
	assert.Equal(t, types.TagsArray{"postgres", "kết nối"}, got.Tags)
	assert.Equal(t, mem.Content, got.Content, "content untouched")
	assert.Equal(t, 2, got.Tier, "tier untouched")
	assert.Len(t, got.Embedding.Slice(), types.MemoryEmbeddingDim, "embedding untouched")

	err = repo.UpdateFields(ctx, "other-tenant", mem.ID, map[string]interface{}{"tier": 1})
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "tenant scoping enforced")
}

func TestPostgresMemoryRepository_AutomatedActorsCannotTouchProtected(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	protected := createPostgresMemory(t, db, "t1", "kb1", "Decided to use pgbouncer", types.VerdictDecision, []float32{1, 0})
	plain := createPostgresMemory(t, db, "t1", "kb1", "Plain note about pools", types.VerdictNone, []float32{0, 1})

	for _, actor := range []string{"dreamer", "system"} {
		ctx := context.WithValue(context.Background(), types.ActorKey{}, actor)
		var pe *types.ErrProtectedVerdict
		assert.ErrorAs(t, repo.Delete(ctx, "t1", protected.ID), &pe, actor+" delete")
		assert.ErrorAs(t, repo.UpdateFields(ctx, "t1", protected.ID, map[string]interface{}{"importance": 1}), &pe, actor+" update")
	}
	still, err := repo.GetByID(context.Background(), "t1", protected.ID)
	require.NoError(t, err, "protected memory survives automated actors")
	assert.Equal(t, types.VerdictDecision, still.Verdict)

	sysCtx := context.WithValue(context.Background(), types.ActorKey{}, "system")
	require.NoError(t, repo.Delete(sysCtx, "t1", plain.ID), "unprotected memories stay deletable")

	require.NoError(t, repo.Delete(context.Background(), "t1", protected.ID), "a human may delete a protected memory")
}
