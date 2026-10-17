package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	_ interfaces.MemoryV2ScopeLock        = (*MemoryRepository)(nil)
	_ interfaces.MemoryV2DreamStateReader = (*MemoryRepository)(nil)
)

// DreamState returns the dreamer state of one scope; nil when it never ran.
func (r *MemoryRepository) DreamState(ctx context.Context, tenantID, kbID string) (*types.DreamerState, error) {
	var states []types.DreamerState
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND kb_id = ?", tenantID, kbID).
		Limit(1).Find(&states).Error; err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	return &states[0], nil
}

// TryScopeLock acquires the dreamer lock of one (tenant, knowledge base)
// scope for ttl. A lock whose holder stopped heartbeating past its expiry is
// taken over. Returns the scope state (run count, last run, stats).
func (r *MemoryRepository) TryScopeLock(
	ctx context.Context, tenantID, kbID, workerID string, ttl time.Duration,
) (bool, *types.DreamerState, error) {
	var acquired bool
	var state types.DreamerState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.DreamerState{
			ID: uuid.NewString(), TenantID: tenantID, KbID: kbID, Stats: []byte("{}"),
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND kb_id = ?", tenantID, kbID).First(&state).Error; err != nil {
			return err
		}
		if state.LockedBy != "" && state.LockedBy != workerID && state.LockedUntil != nil && state.LockedUntil.After(now) {
			return nil
		}
		until := now.Add(ttl)
		state.LockedBy, state.LockedUntil, state.HeartbeatAt = workerID, &until, &now
		acquired = true
		return tx.Model(&types.DreamerState{}).Where("id = ?", state.ID).Updates(map[string]interface{}{
			"locked_by": workerID, "locked_until": until, "heartbeat_at": now, "updated_at": now,
		}).Error
	})
	return acquired, &state, err
}

// HeartbeatScopeLock extends a held lock; false when the lock was lost.
func (r *MemoryRepository) HeartbeatScopeLock(ctx context.Context, tenantID, kbID, workerID string, ttl time.Duration) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.DreamerState{}).
		Where("tenant_id = ? AND kb_id = ? AND locked_by = ?", tenantID, kbID, workerID).
		Updates(map[string]interface{}{"locked_until": now.Add(ttl), "heartbeat_at": now})
	return res.RowsAffected == 1, res.Error
}

// ReleaseScopeLock releases a lock held by workerID and, when ran is true,
// records the run (last_run_at, run count, stats).
func (r *MemoryRepository) ReleaseScopeLock(
	ctx context.Context, tenantID, kbID, workerID string, ran bool, stats map[string]interface{},
) error {
	updates := map[string]interface{}{"locked_by": "", "locked_until": nil, "updated_at": time.Now()}
	if ran {
		updates["last_run_at"] = time.Now()
		updates["runs"] = gorm.Expr("runs + 1")
		if b, err := json.Marshal(stats); err == nil {
			updates["stats"] = b
		}
	}
	return r.db.WithContext(ctx).Model(&types.DreamerState{}).
		Where("tenant_id = ? AND kb_id = ? AND locked_by = ?", tenantID, kbID, workerID).
		Updates(updates).Error
}

// ScopeChangedSince reports whether any memory of the scope was created,
// edited or deleted after t (nil t: always true).
func (r *MemoryRepository) ScopeChangedSince(ctx context.Context, tenantID, kbID string, t *time.Time) (bool, error) {
	if t == nil {
		return true, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Unscoped().Table("agent_memories").
		Where("tenant_id = ? AND kb_id = ?", tenantID, kbID).
		Where("updated_at > ? OR (deleted_at IS NOT NULL AND deleted_at > ?)", *t, *t).
		Limit(1).Count(&n).Error
	return n > 0, err
}

// LeastRecentlyDreamed returns up to limit live memories of the scope, those
// never reviewed by the dreamer first, then the longest ago.
func (r *MemoryRepository) LeastRecentlyDreamed(ctx context.Context, tenantID, kbID string, limit int) ([]*types.AgentMemory, error) {
	var out []*types.AgentMemory
	err := r.db.WithContext(ctx).Table("agent_memories").Select(memoryColumns+", metadata").
		Where("tenant_id = ? AND kb_id = ? AND deleted_at IS NULL", tenantID, kbID).
		Order("COALESCE(metadata->>'dreamed_at', '') ASC, created_at ASC, id").
		Limit(limit).Find(&out).Error
	return out, err
}

// MarkDreamed records that the dreamer reviewed these memories, without
// counting as an edit (the 900081 trigger keeps updated_at).
func (r *MemoryRepository) MarkDreamed(ctx context.Context, tenantID string, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`UPDATE agent_memories
SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{dreamed_at}', to_jsonb(?::text))
WHERE tenant_id = ? AND id IN ? AND deleted_at IS NULL`, at.UTC().Format(time.RFC3339), tenantID, ids).Error
}
