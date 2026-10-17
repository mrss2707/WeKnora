package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type budgetDreamStore struct {
	*dreamStore
	allowed  bool
	err      error
	reserved int
	settled  int
}

func (s *budgetDreamStore) ReserveDreamerTokens(_ context.Context, _, _ string, _ time.Time, tokens, _ int) (bool, error) {
	s.reserved = tokens
	return s.allowed, s.err
}
func (s *budgetDreamStore) SettleDreamerTokens(_ context.Context, _, _ string, tokens int) error {
	s.settled = tokens
	return nil
}

func TestDreamer_SharedBudgetReservationBeforeModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
	}{
		{name: "reserved", allowed: true}, {name: "exhausted"}, {name: "ledger down", err: errors.New("offline")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &budgetDreamStore{dreamStore: newDreamStore(dmem("a", types.VerdictNone, 1)), allowed: tc.allowed, err: tc.err}
			llm := &scriptedChat{reply: `{"actions":[]}`}
			d := newDreamer(store.dreamStore, llm, &recordingApplier{})
			d.repo = store
			result, err := d.RunScopePass(context.Background(), "1", "kb")
			if tc.err != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Greater(t, store.reserved, d.config.TokenBudget, "reservation includes estimated input and maximum output")
			if tc.allowed {
				require.Equal(t, 1, llm.calls)
				require.Equal(t, result.TokenUsed, store.settled)
			} else {
				require.Zero(t, llm.calls, "no unbudgeted model calls")
				require.Zero(t, store.settled)
			}
		})
	}
}
