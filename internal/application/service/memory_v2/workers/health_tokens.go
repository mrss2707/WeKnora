package workers

import (
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/infrastructure/langdata"
	"golang.org/x/text/unicode/norm"
)

func normalizedAssertionText(content string) string {
	return norm.NFC.String(strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(strings.TrimSpace(content))))
}

func assertionTokens(text string, rules *langdata.MemoryAssertionRules) []string {
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' && r != '-' && r != '_'
	})
	var expanded []string
	for _, token := range tokens {
		if replacement, ok := rules.Expansions[token]; ok {
			expanded = append(expanded, strings.Fields(replacement)...)
		} else {
			expanded = append(expanded, token)
		}
	}
	return expanded
}

func containsNegation(content string) bool {
	text := normalizedAssertionText(content)
	rules, ok := langdata.GetMemoryAssertionRules(langdata.DetectMemoryLanguage(text))
	if !ok {
		return false
	}
	for _, token := range assertionTokens(text, rules) {
		if rules.Negations[token] {
			return true
		}
	}
	return false
}
