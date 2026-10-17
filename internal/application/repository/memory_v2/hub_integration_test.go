package repository

import (
	"context"
	"math"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresMemoryRepository_HubScoresCountLiveEdgesOnly(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	ctx := context.Background()
	hub := createPostgresMemory(t, db, "1", "kb", "hub", types.VerdictNone, []float32{1, 0})
	a := createPostgresMemory(t, db, "1", "kb", "a", types.VerdictNone, []float32{1, 0})
	b := createPostgresMemory(t, db, "1", "kb", "b", types.VerdictNone, []float32{1, 0})
	gone := createPostgresMemory(t, db, "1", "kb", "gone", types.VerdictNone, []float32{1, 0})
	lonely := createPostgresMemory(t, db, "1", "kb", "lonely", types.VerdictNone, []float32{1, 0})
	other := createPostgresMemory(t, db, "2", "kb", "other tenant", types.VerdictNone, []float32{1, 0})
	relate(t, db, hub.ID, a.ID, "related_to") // weight 0.8
	relate(t, db, b.ID, hub.ID, "justifies")
	relate(t, db, hub.ID, gone.ID, "related_to")
	require.NoError(t, db.Exec(`UPDATE agent_memories SET deleted_at = now() WHERE id = ?`, gone.ID).Error)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET hub_score = 9 WHERE id IN ?`, []string{lonely.ID, other.ID}).Error)

	require.NoError(t, repo.ComputeHubScores(ctx, "1"))

	score := func(id string) float64 {
		var s float64
		require.NoError(t, db.Raw(`SELECT hub_score FROM agent_memories WHERE id = ?`, id).Scan(&s).Error)
		return s
	}
	assert.InDelta(t, math.Log(3)*0.8, score(hub.ID), 1e-5, "2 live edges; the edge to a deleted memory is ignored")
	assert.InDelta(t, math.Log(2)*0.8, score(a.ID), 1e-5)
	assert.Zero(t, score(lonely.ID), "stale scores reset to 0")
	assert.Equal(t, 9.0, score(other.ID), "other tenants untouched")

	relate(t, db, lonely.ID, a.ID, "related_to")
	require.NoError(t, repo.RefreshHubScores(ctx, "1", []string{lonely.ID}))
	assert.InDelta(t, math.Log(2)*0.8, score(lonely.ID), 1e-5)
	assert.InDelta(t, math.Log(2)*0.8, score(a.ID), 1e-5, "only the listed memories are refreshed")
}
