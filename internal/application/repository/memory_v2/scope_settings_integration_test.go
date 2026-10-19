package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestScopeSettingsUpsertClearAndIsolation(t *testing.T) {
	repo := NewMemoryRepository(memoryV2PostgresDB(t))
	ctx := context.Background()

	missing, err := repo.GetScopeSettings(ctx, "1", "kb")
	require.NoError(t, err)
	require.Nil(t, missing, "no row is not an error")

	at := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.UpsertScopeSettings(ctx, &types.MemoryV2ScopeSettings{TenantID: "1", KbID: "kb", DreamerModelID: "pro", TextModelID: "flash", UpdatedBy: "u1", UpdatedAt: at}))
	require.NoError(t, repo.UpsertScopeSettings(ctx, &types.MemoryV2ScopeSettings{TenantID: "2", KbID: "kb", DreamerModelID: "other", UpdatedBy: "u2", UpdatedAt: at}))

	got, err := repo.GetScopeSettings(ctx, "1", "kb")
	require.NoError(t, err)
	require.Equal(t, "pro", got.DreamerModelID)
	require.Equal(t, "flash", got.TextModelID)
	require.Equal(t, "u1", got.UpdatedBy)

	none, err := repo.GetScopeSettings(ctx, "1", "other-kb")
	require.NoError(t, err)
	require.Nil(t, none, "settings never leak across knowledge bases")

	require.NoError(t, repo.UpsertScopeSettings(ctx, &types.MemoryV2ScopeSettings{TenantID: "1", KbID: "kb", TextModelID: "flash", UpdatedBy: "u3", UpdatedAt: at.Add(time.Minute)}))
	got, err = repo.GetScopeSettings(ctx, "1", "kb")
	require.NoError(t, err)
	require.Empty(t, got.DreamerModelID, "an empty choice is cleared (NULL)")
	require.Equal(t, "u3", got.UpdatedBy)

	other, err := repo.GetScopeSettings(ctx, "2", "kb")
	require.NoError(t, err)
	require.Equal(t, "other", other.DreamerModelID, "tenants are isolated")

	require.Error(t, repo.UpsertScopeSettings(ctx, &types.MemoryV2ScopeSettings{TenantID: "1"}))
}
