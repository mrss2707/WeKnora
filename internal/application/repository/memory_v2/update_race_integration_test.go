package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOptimisticLock_RejectsDifferentMicrosecondInSameMillisecond(t *testing.T) {
	db := memoryV2PostgresDB(t)
	m := createPostgresMemory(t, db, "1", "kb", "timestamp version test", types.VerdictNone, []float32{1, 0})
	current := time.Date(2026, 10, 18, 1, 0, 0, 123900000, time.UTC)
	stale := current.Add(-800 * time.Microsecond)
	require.NoError(t, db.Exec("UPDATE agent_memories SET updated_at = ? WHERE id = ?", current, m.ID).Error)
	err := NewMemoryRepository(db).UpdateFieldsIf(context.Background(), "1", m.ID, map[string]interface{}{"importance": 5}, &stale)
	var conflict *types.MemoryV2ConflictError
	require.ErrorAs(t, err, &conflict)
	got, err := NewMemoryRepository(db).GetByID(context.Background(), "1", m.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.Importance)
}

func TestAutomatedUpdate_CannotRaceHumanProtection(t *testing.T) {
	db := memoryV2PostgresDB(t)
	m := createPostgresMemory(t, db, "1", "kb", "protected verdict race test", types.VerdictNone, []float32{1, 0})
	var once sync.Once
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("memory_v2_test:protect", func(_ *gorm.DB) {
		once.Do(func() {
			require.NoError(t, db.Exec("UPDATE agent_memories SET verdict = 'decision' WHERE id = ?", m.ID).Error)
		})
	}))
	c := context.WithValue(context.Background(), types.ActorKey{}, "dreamer")
	err := NewMemoryRepository(db).UpdateFieldsIf(c, "1", m.ID, map[string]interface{}{"importance": 5}, nil)
	var protected *types.ErrProtectedVerdict
	require.ErrorAs(t, err, &protected)
	got, err := NewMemoryRepository(db).GetByID(context.Background(), "1", m.ID)
	require.NoError(t, err)
	require.Equal(t, types.VerdictDecision, got.Verdict)
	require.Equal(t, 1, got.Importance)
}
