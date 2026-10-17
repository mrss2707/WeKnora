package memory_v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var (
	_ interfaces.MemoryV2DreamPreviewer    = (*MemoryServiceV2Impl)(nil)
	_ interfaces.MemoryV2DreamStatusReader = (*MemoryServiceV2Impl)(nil)
)

// PreviewDreamScope returns the dreamer's proposals for one knowledge base
// without applying them, marking memories reviewed or counting a run.
func (s *MemoryServiceV2Impl) PreviewDreamScope(ctx context.Context, tenantID, kbID string) (*types.DreamResult, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2: %s", reason)
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(kbID) == "" {
		return nil, &types.ErrMemoryValidation{Message: "tenant and knowledge base are required"}
	}
	s.initWorkers()
	if s.dreamer == nil {
		return &types.DreamResult{Actions: []types.DreamAction{}}, nil
	}
	return s.dreamer.PreviewScopePass(ctx, tenantID, kbID)
}

// DreamStatus reports the dreamer configuration and last run of one
// knowledge base.
func (s *MemoryServiceV2Impl) DreamStatus(ctx context.Context, tenantID, kbID string) (*types.DreamStatus, error) {
	cfg := s.dreamerConfig()
	st := &types.DreamStatus{
		Enabled:       cfg.Enabled,
		Scheduled:     cfg.Enabled && s.config.WorkerMutations,
		DryRun:        cfg.DryRun,
		Interval:      cfg.Interval,
		MergeLiveRuns: cfg.MergeLiveAfterRuns,
	}
	reader, ok := s.repo.(interfaces.MemoryV2DreamStateReader)
	if !ok {
		return st, nil
	}
	state, err := reader.DreamState(ctx, tenantID, kbID)
	if err != nil || state == nil {
		return st, err
	}
	st.Runs = state.Runs
	st.LastRunAt = state.LastRunAt
	st.Running = state.LockedBy != "" && state.LockedUntil != nil && state.LockedUntil.After(time.Now())
	st.MergeLive = cfg.MergeLiveAfterRuns > 0 && state.Runs >= cfg.MergeLiveAfterRuns
	if len(state.Stats) > 0 {
		var stats map[string]interface{}
		if json.Unmarshal(state.Stats, &stats) == nil && len(stats) > 0 {
			st.LastStats = stats
		}
	}
	return st, nil
}
