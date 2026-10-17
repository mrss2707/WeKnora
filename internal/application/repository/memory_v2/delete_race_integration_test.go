package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutomatedFullWriteAndDelete_CannotRaceHumanProtection(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			db := memoryV2PostgresDB(t)
			m := createPostgresMemory(t, db, "1", "kb", "human protects before worker writes", types.VerdictNone, []float32{1, 0})
			var once sync.Once
			protect := func(tx *gorm.DB) {
				once.Do(func() {
					require.NoError(t, tx.Exec("UPDATE agent_memories SET verdict = 'decision' WHERE id = ?", m.ID).Error)
				})
			}
			if operation == "update" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("memory_v2_test:protect_full", protect))
			} else {
				require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("memory_v2_test:protect_delete", protect))
			}
			c := context.WithValue(context.Background(), types.ActorKey{}, "system")
			repo := NewMemoryRepository(db)
			var err error
			if operation == "update" {
				m.Importance = 5
				err = repo.Update(c, m)
			} else {
				err = repo.Delete(c, "1", m.ID)
			}
			var protected *types.ErrProtectedVerdict
			require.ErrorAs(t, err, &protected)
			got, err := repo.GetByID(context.Background(), "1", m.ID)
			require.NoError(t, err)
			require.Equal(t, 1, got.Importance)
			if operation == "update" {
				require.Equal(t, types.VerdictDecision, got.Verdict)
			}
		})
	}
}
