package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGraphJobs_ConcurrentReplicasClaimEachRowOnce(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	c := context.Background()
	for range 12 {
		m := createPostgresMemory(t, db, "1", "kb", "concurrent leased graph job", types.VerdictNone, []float32{1, 0})
		require.NoError(t, repo.EnqueueGraphLink(c, m))
	}
	var schema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&schema).Error)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := []string{}
	failures := []error{}
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SET LOCAL search_path TO " + schema + ", public").Error; err != nil {
					return err
				}
				job, err := NewMemoryRepository(tx).ClaimGraphLink(c, uuid.NewString(), time.Minute)
				if err != nil {
					return err
				}
				if job != nil {
					mu.Lock()
					claimed = append(claimed, job.ID)
					mu.Unlock()
				}
				return nil
			})
			if err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Empty(t, failures)
	require.Len(t, claimed, 12)
	unique := map[string]bool{}
	for _, id := range claimed {
		require.False(t, unique[id], "one live lease per row across replicas")
		unique[id] = true
	}
}
