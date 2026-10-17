package repository

import (
	"context"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2StatsReader = (*MemoryRepository)(nil)

// Stats aggregates the live memories of a scope (whole tenant when kbID is
// empty) with one grouped query.
func (r *MemoryRepository) Stats(ctx context.Context, tenantID, kbID string) (*types.MemoryStats, error) {
	type row struct {
		MemoryType string
		Tier       int
		Verdict    string
		N          int64
		SumImp     float64
		SumHub     float64
	}
	q := r.db.WithContext(ctx).Table("agent_memories").
		Select("memory_type, tier, verdict, COUNT(*) AS n, COALESCE(SUM(importance),0) AS sum_imp, COALESCE(SUM(hub_score),0) AS sum_hub").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if kbID != "" {
		q = q.Where("kb_id = ?", kbID)
	}
	var rows []row
	if err := q.Group("memory_type, tier, verdict").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &types.MemoryStats{ByType: map[string]int64{}, ByTier: map[string]int64{}, ByVerdict: map[string]int64{}}
	var sumImp, sumHub float64
	for _, rw := range rows {
		st.TotalMemories += rw.N
		st.ByType[rw.MemoryType] += rw.N
		st.ByTier[strconv.Itoa(rw.Tier)] += rw.N
		st.ByVerdict[rw.Verdict] += rw.N
		sumImp += rw.SumImp
		sumHub += rw.SumHub
	}
	if st.TotalMemories > 0 {
		st.AvgImportance = sumImp / float64(st.TotalMemories)
		st.AvgHubScore = sumHub / float64(st.TotalMemories)
	}
	return st, nil
}
