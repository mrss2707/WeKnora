package memory_v2

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestSaveMemory_ContendedNearMergePreservesIncomingFact(t *testing.T) {
	repo := newIngestionRepo()
	existing := &types.AgentMemory{ID: "existing", TenantID: "tenant-1", KbID: "kb", Content: "original related fact", Verdict: types.VerdictNone, UpdatedAt: time.Now()}
	repo.memories[existing.ID] = existing
	repo.cosineResults = []*types.MemorySearchResult{{Memory: existing, Score: 0.95}}
	repo.updateErr = &types.MemoryV2ConflictError{Message: "a competing merge changed the version"}
	svc := newIngestionService(repo)
	result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", KbID: "kb", Content: "new fact must survive concurrent near merges"})
	require.NoError(t, err)
	require.True(t, result.Created)
	require.NotEqual(t, existing.ID, result.Memory.ID)
	require.Equal(t, "original related fact", existing.Content)
	require.GreaterOrEqual(t, repo.cosineCalls, 3, "fresh candidates are checked before conservative fallback")
}

func TestExactCreateRace_ReturnsTheCommittedScopedWinner(t *testing.T) {
	repo := newIngestionRepo()
	fp := computeScopedFingerprint("tenant-1", "kb", "identical concurrent content")
	existing := &types.AgentMemory{ID: "winner", TenantID: "tenant-1", KbID: "kb", Fingerprint: &fp}
	repo.memories[existing.ID] = existing
	svc := newIngestionService(repo)
	candidate := &types.AgentMemory{TenantID: "tenant-1", KbID: "kb", Fingerprint: &fp}
	result := svc.recoverExactCreateRace(context.Background(), candidate, &pgconn.PgError{Code: "23505"})
	require.NotNil(t, result)
	require.False(t, result.Created)
	require.Equal(t, "winner", result.Memory.ID)
	candidate.KbID = "other-kb"
	require.Nil(t, svc.recoverExactCreateRace(context.Background(), candidate, &pgconn.PgError{Code: "23505"}))
}
