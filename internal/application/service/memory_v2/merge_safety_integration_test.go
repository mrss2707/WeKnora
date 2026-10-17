package memory_v2

import (
	"context"
	"errors"
	"strings"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMergeAndUndo_RestoreOriginalRelationsAndVectorAtomically(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	c := context.Background()
	a := createPGServiceMemory(t, db, "1", "kb", "survivor original content", []float32{1, 0})
	b := createPGServiceMemory(t, db, "1", "kb", "loser distinct content", []float32{1, 0})
	neighbour := createPGServiceMemory(t, db, "1", "kb", "loser neighbour content", []float32{0, 1})
	other := createPGServiceMemory(t, db, "1", "kb", "survivor neighbour content", []float32{0, 1})
	require.NoError(t, repo.CreateRelation(c, &types.MemoryRelation{TenantID: "1", FromUUID: b.ID, ToUUID: neighbour.ID, RelationType: "related_to", Weight: 1}))
	require.NoError(t, repo.CreateRelation(c, &types.MemoryRelation{TenantID: "1", FromUUID: a.ID, ToUUID: other.ID, RelationType: "related_to", Weight: 1}))
	svc := NewMemoryServiceV2(repo, nil, types.DefaultMemoryV2Config(), nil)
	svc.embedder = &mockEmbedder{embedFunc: func(_ context.Context, text string) ([]float32, error) {
		if strings.Contains(text, mergeSeparator) {
			return []float32{0.6, 0.8}, nil
		}
		return []float32{1, 0}, nil
	}}
	_, err := svc.MergeInto(c, a, []*types.AgentMemory{b})
	require.NoError(t, err)
	merged, err := repo.GetByID(c, "1", a.ID)
	require.NoError(t, err)
	require.InDelta(t, 0.6, merged.Embedding.Slice()[0], 0.00001)
	relations, err := repo.GetRelations(c, a.ID, "1")
	require.NoError(t, err)
	require.Len(t, relations, 2)
	// A failed model call must leave both the merge and its graph intact.
	svc.embedder = &mockEmbedder{embedFunc: func(context.Context, string) ([]float32, error) { return nil, errors.New("model down") }}
	_, err = svc.UndoMerge(c, "1", "kb", a.ID)
	var unavailable *types.MemoryV2ModelUnavailable
	require.ErrorAs(t, err, &unavailable)
	unchanged, err := repo.GetByID(c, "1", a.ID)
	require.NoError(t, err)
	require.Equal(t, merged.Content, unchanged.Content)
	svc.embedder = &mockEmbedder{embedFunc: func(context.Context, string) ([]float32, error) { return []float32{1, 0}, nil }}
	restored, err := svc.UndoMerge(c, "1", "kb", a.ID)
	require.NoError(t, err)
	require.Equal(t, "survivor original content", restored.Content)
	require.InDelta(t, 1, restored.Embedding.Slice()[0], 0.00001)
	require.InDelta(t, 0, restored.Embedding.Slice()[1], 0.00001)
	_, err = repo.GetByID(c, "1", b.ID)
	require.NoError(t, err)
	relations, err = repo.GetRelations(c, a.ID, "1")
	require.NoError(t, err)
	require.Len(t, relations, 1, "generated survivor edge removed, preexisting edge retained")
	require.Equal(t, other.ID, relations[0].ToUUID)
	relations, err = repo.GetRelations(c, b.ID, "1")
	require.NoError(t, err)
	require.Len(t, relations, 1, "original loser edge restored")
	require.Equal(t, neighbour.ID, relations[0].ToUUID)
}

func TestMerge_RejectsParticipantChangedDuringEmbedding(t *testing.T) {
	for _, target := range []string{"survivor", "loser"} {
		t.Run(target, func(t *testing.T) {
			db := pgServiceDB(t)
			repo := repository.NewMemoryRepository(db)
			a := createPGServiceMemory(t, db, "1", "kb", "first original content", []float32{1, 0})
			b := createPGServiceMemory(t, db, "1", "kb", "second original content", []float32{1, 0})
			svc := NewMemoryServiceV2(repo, nil, types.DefaultMemoryV2Config(), nil)
			id := a.ID
			if target == "loser" {
				id = b.ID
			}
			svc.embedder = &mockEmbedder{embedFunc: func(context.Context, string) ([]float32, error) {
				require.NoError(t, repo.UpdateFieldsIf(context.Background(), "1", id, map[string]interface{}{"importance": 5}, nil))
				return []float32{1, 0}, nil
			}}
			_, err := svc.MergeInto(context.Background(), a, []*types.AgentMemory{b})
			var conflict *types.MemoryV2ConflictError
			require.ErrorAs(t, err, &conflict)
			after, err := repo.GetByID(context.Background(), "1", id)
			require.NoError(t, err)
			require.Equal(t, 5, after.Importance)
			require.NotContains(t, after.Content, mergeSeparator)
			_, err = repo.GetByID(context.Background(), "1", b.ID)
			require.NoError(t, err, "loser not deleted on conflict")
		})
	}
}
