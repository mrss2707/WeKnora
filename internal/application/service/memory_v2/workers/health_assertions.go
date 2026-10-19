package workers

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/langdata"
	"github.com/Tencent/WeKnora/internal/types"
)

// assertionKey preserves the entire assertion, including its subject and word
// order. Topic/tag overlap alone is never evidence of opposite assertions.
func assertionKey(content string) (key string, negative bool, supported bool) {
	if len(content) > 1024 {
		return "", false, false
	}
	text := normalizedAssertionText(content)
	text = strings.TrimSuffix(text, ".")
	if strings.ContainsAny(text, ".!?;:,\n\r") {
		return "", false, false
	}
	code := langdata.DetectMemoryLanguage(text)
	rules, ok := langdata.GetMemoryAssertionRules(code)
	if !ok {
		return "", false, false
	}
	expanded := assertionTokens(text, rules)
	if len(expanded) < 3 || len(expanded) > 64 {
		return "", false, false
	}
	if rules.LeadingAmbiguous[expanded[0]] {
		return "", false, false
	}
	joined := " " + strings.Join(expanded, " ") + " "
	for _, phrase := range rules.AmbiguousPhrases {
		if strings.Contains(joined, " "+phrase+" ") {
			return "", false, false
		}
	}
	var assertion []string
	negations, substantive := 0, 0
	stopwords := langdata.Get(code).Stopwords
	for i, token := range expanded {
		if rules.Ambiguous[token] {
			return "", false, false
		}
		if rules.Negations[token] {
			// Leading negation can change the subject rather than the predicate.
			if i == 0 || i == len(expanded)-1 {
				return "", false, false
			}
			negations++
			continue
		}
		assertion = append(assertion, token)
		if !stopwords[token] {
			substantive++
		}
	}
	if negations > 1 || substantive < 2 {
		return "", false, false
	}
	return code + ":" + strings.Join(assertion, " "), negations == 1, true
}

func contradictionCandidates(ctx context.Context, memories []*types.AgentMemory) ([]*types.MemoryHealthIssue, int, error) {
	type scopeKey struct{ tenant, kb, assertion string }
	type polarities struct{ positive, negative *types.AgentMemory }
	seen := map[scopeKey]polarities{}
	var issues []*types.MemoryHealthIssue
	supported := 0
	for _, memory := range memories {
		if err := ctx.Err(); err != nil {
			return nil, supported, err
		}
		if memory == nil || memory.DeletedAt.Valid || memory.Verdict == types.VerdictRefuted {
			continue
		}
		assertion, negative, ok := assertionKey(memory.Content)
		if !ok {
			continue
		}
		supported++
		key := scopeKey{memory.TenantID, memory.KbID, assertion}
		pair := seen[key]
		opposite := pair.negative
		if negative {
			opposite = pair.positive
		}
		if opposite != nil && opposite.ID != memory.ID {
			issues = append(issues, &types.MemoryHealthIssue{
				Type: "contradiction", MemoryID: opposite.ID,
				Description: fmt.Sprintf("Potential opposite-assertion candidate between memories %s and %s", opposite.ID, memory.ID),
				Severity:    "medium", Suggestion: "Review the assertions in context; this lexical candidate is not a verified contradiction",
			})
		}
		if negative && pair.negative == nil {
			pair.negative = memory
		} else if !negative && pair.positive == nil {
			pair.positive = memory
		}
		seen[key] = pair
	}
	return issues, supported, nil
}
