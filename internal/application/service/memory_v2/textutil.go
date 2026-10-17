package memory_v2

import "unicode/utf8"

// maxContentChars is the longest memory content accepted by validateContent.
const maxContentChars = 10000

// truncateRunes shortens s to at most maxRunes runes without splitting a
// multi-byte character (byte slicing corrupts Vietnamese/CJK text and makes
// PostgreSQL reject the value as invalid UTF-8).
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
