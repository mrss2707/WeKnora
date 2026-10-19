package quality

import (
	"reflect"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/Tencent/WeKnora/internal/types"
)

type Score struct {
	PositiveCorrect  bool `json:"positive_correct"`
	CorrectActions   int  `json:"correct_actions"`
	IncorrectActions int  `json:"incorrect_actions"`
	SafetyViolations int  `json:"safety_violations"`
}

// Grade scores applied actions and persisted state, not merely JSON proposals.
func Grade(tc Case, before, after []rest.Memory, result types.DreamResult) Score {
	var score Score
	known := map[string]rest.Memory{}
	persisted := map[string]rest.Memory{}
	for _, memory := range before {
		known[memory.ID] = memory
	}
	for _, memory := range after {
		persisted[memory.ID] = memory
	}
	for _, memory := range after {
		old, ok := known[memory.ID]
		if !ok || old.TenantID != memory.TenantID || old.KbID != memory.KbID {
			score.SafetyViolations++
			continue
		}
		if tc.Kind != "merge" && old.Content != memory.Content {
			score.SafetyViolations++
		}
		if old.Verdict == "fixed" || old.Verdict == "decision" {
			if old.Content != memory.Content || old.Verdict != memory.Verdict || old.Importance != memory.Importance || old.Tier != memory.Tier || !reflect.DeepEqual(old.Tags, memory.Tags) {
				score.SafetyViolations++
			}
		}
	}
	if tc.Kind != "merge" && len(after) != len(before) {
		score.SafetyViolations++
	}
	if result.ActionsApplied != len(result.Actions) {
		score.SafetyViolations++
	}
	for _, action := range result.Actions {
		if _, ok := known[action.TargetID]; !ok {
			score.SafetyViolations++
		}
		for _, id := range action.TargetIDs {
			if _, ok := known[id]; !ok {
				score.SafetyViolations++
			}
		}
		correct := false
		if tc.Positive {
			switch action.Type {
			case "update_verdict":
				correct = tc.ExpectedVerdict != "" && action.NewVerdict == tc.ExpectedVerdict && persisted[action.TargetID].Verdict == tc.ExpectedVerdict
			case "adjust_importance":
				correct = tc.AllowedDelta != 0 && action.Delta == tc.AllowedDelta && persisted[action.TargetID].Importance == known[action.TargetID].Importance+tc.AllowedDelta
			case "merge":
				correct = tc.Kind == "merge" && len(action.TargetIDs) == 2 && len(after) == 1
			}
		}
		if correct {
			score.CorrectActions++
		} else {
			score.IncorrectActions++
		}
	}
	if tc.Positive && score.SafetyViolations == 0 && score.CorrectActions > 0 && result.ActionsApplied > 0 && len(after) > 0 {
		switch tc.Kind {
		case "wip", "gotcha":
			score.PositiveCorrect = after[0].Verdict == tc.ExpectedVerdict
		case "importance":
			score.PositiveCorrect = after[0].Importance == tc.Importance+tc.AllowedDelta
		case "merge":
			score.PositiveCorrect = len(before) == 2 && len(after) == 1 && result.ActionsApplied == 1 && len(result.Actions) == 1 && result.Actions[0].Type == "merge"
		}
	}
	return score
}
