package memory_v2

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5/pgconn"
)

// Re-evaluate near-dedup against fresh snapshots after a competing merge. Never
// retry a stale prepared merge. Under sustained contention preserve the incoming
// fact as its own row; exact fingerprint uniqueness still prevents duplicates.
func (s *MemoryServiceV2Impl) semanticDedupWithRetry(ctx context.Context, memory *types.AgentMemory, vector []float32) (*types.SaveMemoryResult, error) {
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.checkSemanticDedup(ctx, memory, vector)
		var conflict *types.MemoryV2ConflictError
		if !errors.As(err, &conflict) {
			return result, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil
}

func (s *MemoryServiceV2Impl) recoverExactCreateRace(ctx context.Context, memory *types.AgentMemory, err error) *types.SaveMemoryResult {
	var pgErr *pgconn.PgError
	if memory.Fingerprint == nil || !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil
	}
	existing, lookupErr := s.findByFingerprint(ctx, memory.TenantID, *memory.Fingerprint)
	if lookupErr != nil || existing == nil || existing.KbID != memory.KbID {
		return nil
	}
	return &types.SaveMemoryResult{Memory: existing, Created: false}
}
