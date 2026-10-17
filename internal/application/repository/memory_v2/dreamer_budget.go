package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2DreamerBudget = (*MemoryRepository)(nil)

func (r *MemoryRepository) ReserveDreamerTokens(ctx context.Context, tenantID, reservationID string, day time.Time, tokens, limit int) (bool, error) {
	if tokens <= 0 || limit <= 0 || tenantID == "" || reservationID == "" {
		return false, fmt.Errorf("invalid dreamer budget reservation")
	}
	date := day.UTC().Format("2006-01-02")
	acquired := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The same tenant/day lock serializes reservations made by different KBs
		// and replicas. It is held only around the ledger insert, never the LLM.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "memory-v2-budget/"+tenantID+"/"+date).Error; err != nil {
			return err
		}
		var used int64
		if err := tx.Raw(`SELECT COALESCE(SUM(tokens), 0) FROM memory_v2_dreamer_budget WHERE tenant_id = ? AND budget_day = ?`, tenantID, date).Scan(&used).Error; err != nil {
			return err
		}
		if int64(tokens) > int64(limit)-used {
			return nil
		}
		result := tx.Exec(`INSERT INTO memory_v2_dreamer_budget (reservation_id, tenant_id, budget_day, tokens)
   VALUES (?, ?, ?, ?) ON CONFLICT (reservation_id) DO NOTHING`, reservationID, tenantID, date, tokens)
		acquired = result.RowsAffected == 1
		return result.Error
	})
	return acquired, err
}

func (r *MemoryRepository) SettleDreamerTokens(ctx context.Context, tenantID, reservationID string, tokens int) error {
	if tokens < 0 {
		return fmt.Errorf("negative dreamer token usage")
	}
	// Compare-and-set prevents double settlement and refunds. A crashed or
	// uncertain model call leaves its original reservation charged for the day.
	return r.db.WithContext(ctx).Exec(`UPDATE memory_v2_dreamer_budget SET tokens = ?, settled = TRUE
  WHERE reservation_id = ? AND tenant_id = ? AND settled = FALSE`, tokens, reservationID, tenantID).Error
}
