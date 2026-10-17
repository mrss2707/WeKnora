package memory_v2

import (
	"context"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRelink_ReembedsLegacyAndMismatchedVectorsWithVerifiedModel(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	legacy := createPGServiceMemory(t, db, "1", "kb-1", "legacy protected architecture decision", []float32{1, 0})
	mismatch := createPGServiceMemory(t, db, "1", "kb-1", "previous model deployment note", []float32{0, 1})
	foreign := createPGServiceMemory(t, db, "2", "kb-2", "other tenant private deployment note", []float32{0, 1})
	require.NoError(t, db.Exec("UPDATE agent_memories SET verdict='decision', tier=0 WHERE id=?", legacy.ID).Error)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET metadata='{"embedding_model_id":"old-model"}' WHERE id=?`, mismatch.ID).Error)
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)
	svc.resolver, _ = newResolverFixture()
	c := context.Background()
	result, err := svc.RelinkKnowledgeBase(c, "1", "kb-1")
	require.NoError(t, err)
	require.Equal(t, 2, result.Reembedded)
	require.Equal(t, 2, result.Memories)
	after, err := repo.GetByID(c, "1", legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "kb1-emb", metadataString(after.Metadata, metadataEmbeddingModel))
	require.Equal(t, types.VerdictDecision, after.Verdict)
	require.Equal(t, 0, after.Tier)
	require.Equal(t, legacy.Content, after.Content)
	other, err := repo.GetByID(c, "2", foreign.ID)
	require.NoError(t, err)
	require.Empty(t, metadataString(other.Metadata, metadataEmbeddingModel), "another tenant is untouched")
	second, err := svc.RelinkKnowledgeBase(c, "1", "kb-1")
	require.NoError(t, err)
	require.Zero(t, second.Reembedded, "backfill is idempotent")
	entries, total, err := repo.ListAudit(c, types.MemoryAuditFilter{TenantID: "1", KbID: "kb-1", Actions: []string{types.MemoryAuditPatched}, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, entries, 2)
}
