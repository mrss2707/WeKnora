// Package langdata provides a registry for language-specific data used across
// the WeKnora codebase: stopwords, question words, chapter detection patterns,
// page footer patterns, and character-per-token ratios.
//
// Languages register their data at init-time via Register(). Consumers look
// up data via Get(), which returns the registered data for the given language
// code or a fallback default when no registration matches.
package langdata

import (
	"regexp"
	"sync"
)

// LanguageData holds language-specific text processing data.
type LanguageData struct {
	// Code is the ISO 639-1 language code (e.g. "en", "zh", "vi").
	Code string

	// Stopwords is a set of common words that carry little semantic weight.
	// Keys are lowercase; the value is always true.
	Stopwords map[string]bool

	// QuestionWords lists interrogative words/phrases that should be stripped
	// during query expansion (e.g. "what", "how", "why" in English).
	QuestionWords []string

	// ChapterPatterns are compiled regular expressions that match chapter /
	// section heading lines in the target language (e.g. "Chapter 1:", "第一章").
	ChapterPatterns []*regexp.Regexp

	// PageFooterPattern matches page footer / page number lines (e.g.
	// "Page 1 of 10", "Trang 5", "第 3 页").
	PageFooterPattern *regexp.Regexp

	// CharsPerToken is the approximate number of characters per token for this
	// language, used to estimate token counts without a real tokenizer. Values
	// are conservative (slightly over-estimate) so chunks stay under model limits.
	CharsPerToken float64
}

// registry is the global, thread-safe language data store.
var (
	mu       sync.RWMutex
	registry = make(map[string]*LanguageData)
)

// Register adds or replaces language data for the given language code.
// Typically called from init() functions in lang_*.go files.
func Register(data *LanguageData) {
	mu.Lock()
	defer mu.Unlock()
	registry[data.Code] = data
}

// Get returns the registered LanguageData for code. If no data is registered
// for code, it returns the mixed-language fallback defaults.
func Get(code string) *LanguageData {
	mu.RLock()
	defer mu.RUnlock()
	if data, ok := registry[code]; ok {
		return data
	}
	// Fallback to mixed defaults (must be registered at init-time by lang_mixed.go).
	if data, ok := registry["mixed"]; ok {
		return data
	}
	// Last-resort: return a minimal empty struct so callers never get nil.
	return &LanguageData{
		Code:          code,
		Stopwords:     make(map[string]bool),
		CharsPerToken: 3.0,
	}
}
