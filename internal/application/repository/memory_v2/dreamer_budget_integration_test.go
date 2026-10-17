package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDreamerBudget_ConcurrentReplicasAndRestart(t *testing.T) {
	db := memoryV2PostgresDB(t)
	raw, err := os.ReadFile("../../../../migrations/modules/memory_v2/postgres/900082_memory_v2_dreamer_budget.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(raw)).Error)
	var schema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&schema).Error)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(8)
	c := context.Background()
	day := time.Now().UTC()
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := []string{}
	errs := []error{}
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := uuid.NewString()
			// Pin the generated test schema on every pooled connection, just as
			// independent app replicas target the same production schema.
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SET LOCAL search_path TO " + schema + ", public").Error; err != nil {
					return err
				}
				ok, err := NewMemoryRepository(tx).ReserveDreamerTokens(c, "tenant-a", id, day, 100, 300)
				if ok {
					mu.Lock()
					accepted = append(accepted, id)
					mu.Unlock()
				}
				return err
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Empty(t, errs)
	require.Len(t, accepted, 3, "replicas must share one tenant-wide cap")
	// Use a pinned transaction for assertions too, after the pool has expanded.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("SET LOCAL search_path TO "+schema+", public").Error)
		repo := NewMemoryRepository(tx) // fresh instance simulates process restart
		ok, err := repo.ReserveDreamerTokens(c, "tenant-a", uuid.NewString(), day, 1, 300)
		require.NoError(t, err)
		require.False(t, ok, "crashed reservations stay charged")
		require.NoError(t, repo.SettleDreamerTokens(c, "tenant-b", accepted[0], 0))
		ok, err = repo.ReserveDreamerTokens(c, "tenant-a", uuid.NewString(), day, 1, 300)
		require.NoError(t, err)
		require.False(t, ok, "another tenant cannot refund a reservation")
		require.NoError(t, repo.SettleDreamerTokens(c, "tenant-a", accepted[0], 40))
		require.NoError(t, repo.SettleDreamerTokens(c, "tenant-a", accepted[0], 0))
		ok, err = repo.ReserveDreamerTokens(c, "tenant-a", uuid.NewString(), day, 61, 300)
		require.NoError(t, err)
		require.False(t, ok, "settlement is applied only once")
		ok, err = repo.ReserveDreamerTokens(c, "tenant-a", uuid.NewString(), day, 60, 300)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = repo.ReserveDreamerTokens(c, "tenant-b", uuid.NewString(), day, 100, 300)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = repo.ReserveDreamerTokens(c, "tenant-a", uuid.NewString(), day.AddDate(0, 0, 1), 100, 300)
		require.NoError(t, err)
		require.True(t, ok)
		return nil
	}))
}
