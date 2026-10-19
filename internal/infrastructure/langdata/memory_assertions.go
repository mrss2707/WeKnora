package langdata

import "sync"

// MemoryAssertionRules describes conservative lexical assertions, not semantic
// entailment. Rules are registered per language rather than guessed by callers.
type MemoryAssertionRules struct {
	Negations        map[string]bool
	Expansions       map[string]string
	Ambiguous        map[string]bool
	LeadingAmbiguous map[string]bool
	AmbiguousPhrases []string
}

var assertionRulesMu sync.RWMutex
var assertionRules = map[string]*MemoryAssertionRules{}

func RegisterMemoryAssertionRules(code string, rules *MemoryAssertionRules) {
	assertionRulesMu.Lock()
	defer assertionRulesMu.Unlock()
	assertionRules[code] = rules
}

// GetMemoryAssertionRules deliberately has no fallback for unsupported languages.
func GetMemoryAssertionRules(code string) (*MemoryAssertionRules, bool) {
	assertionRulesMu.RLock()
	defer assertionRulesMu.RUnlock()
	rules, ok := assertionRules[code]
	return rules, ok
}
