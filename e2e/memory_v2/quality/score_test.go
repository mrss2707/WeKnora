package quality

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCorpusHasFixedBalancedLabels(t *testing.T) {
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, tc := range Corpus() {
		require.False(t, seen[tc.ID])
		seen[tc.ID] = true
		require.LessOrEqual(t, utf8.RuneCountInString(tc.Content), 300, "the production prompt must not silently truncate an evaluation case")
		key := tc.Language + "-negative"
		if tc.Positive {
			key = tc.Language + "-positive"
		}
		counts[key]++
	}
	require.Equal(t, map[string]int{"en-positive": 50, "vi-positive": 50, "en-negative": 50, "vi-negative": 50}, counts)
}

func TestHoldoutCorpusIsSeparateAndBalanced(t *testing.T) {
	development := map[string]bool{}
	for _, tc := range Corpus() {
		development[tc.ID] = true
		development[tc.Content] = true
	}
	counts := map[string]int{}
	for _, tc := range HoldoutCorpus() {
		require.False(t, development[tc.ID])
		require.False(t, development[tc.Content])
		require.LessOrEqual(t, utf8.RuneCountInString(tc.Content), 300)
		key := tc.Language + "-negative"
		if tc.Positive {
			key = tc.Language + "-positive"
		}
		counts[key]++
	}
	require.Equal(t, map[string]int{"en-positive": 50, "vi-positive": 50, "en-negative": 50, "vi-negative": 50}, counts)
}

func TestGradeRequiresAppliedPersistedAction(t *testing.T) {
	tc := Case{Positive: true, Kind: "wip", ExpectedVerdict: "wip"}
	before := []rest.Memory{{ID: "a", TenantID: "tenant", KbID: "kb", Content: "unfinished task", Verdict: "none"}}
	result := types.DreamResult{ActionsProposed: 1, ActionsApplied: 1, Actions: []types.DreamAction{{Type: "update_verdict", TargetID: "a", NewVerdict: "wip"}}}
	grade := Grade(tc, before, before, result)
	require.False(t, grade.PositiveCorrect, "a reported action without a persisted change is not success")
	require.Equal(t, 1, grade.IncorrectActions)
	after := append([]rest.Memory(nil), before...)
	after[0].Verdict = "wip"
	grade = Grade(tc, before, after, result)
	require.True(t, grade.PositiveCorrect)
	require.Equal(t, 1, grade.CorrectActions)
	require.Zero(t, grade.SafetyViolations)
	grade = Grade(tc, before, before, types.DreamResult{})
	require.False(t, grade.PositiveCorrect, "zero action is never a positive-case success")
}

func TestGradeRejectsProtectedAndForeignChanges(t *testing.T) {
	before := []rest.Memory{{ID: "a", TenantID: "tenant", KbID: "kb", Content: "protected fact", Verdict: "fixed", Importance: 3}}
	after := append([]rest.Memory(nil), before...)
	after[0].Importance++
	require.Positive(t, Grade(Case{}, before, after, types.DreamResult{}).SafetyViolations)
	after[0].KbID = "foreign"
	require.Positive(t, Grade(Case{}, before, after, types.DreamResult{}).SafetyViolations)
	encoded, err := json.Marshal(Corpus())
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
}
