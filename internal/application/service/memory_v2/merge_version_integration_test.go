package memory_v2

import (
	"context"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestMerge_ReturnedVersionCanBePatchedImmediately(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	a := createPGServiceMemory(t, db, "1", "kb", "survivor content version", []float32{1, 0})
	b := createPGServiceMemory(t, db, "1", "kb", "additional content version", []float32{1, 0})
	svc := NewMemoryServiceV2(repo, nil, types.DefaultMemoryV2Config(), nil)
	svc.embedder = &mockEmbedder{}
	c := context.Background()
	merged, err := svc.MergeInto(c, a, []*types.AgentMemory{b})
	require.NoError(t, err)
	tags := []string{"immediate-patch"}
	_, err = svc.PatchMemory(c, "1", merged.ID, interfaces.MemoryV2Patch{Tags: &tags, ExpectedUpdatedAt: &merged.UpdatedAt})
	require.NoError(t, err, "a successful merge must not return an already-stale optimistic version")
}
