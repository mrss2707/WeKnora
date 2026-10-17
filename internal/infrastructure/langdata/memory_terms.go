package langdata

import (
	"strings"
	"sync"
	"unicode"
)

// MemoryTerms are the per-language cues Memory V2 uses to classify a memory
// (episodic, semantic, procedural, decision, preference) and score its
// importance. Kept beside LanguageData rather than inside it so the shared
// struct stays unchanged.
type MemoryTerms struct {
	Code string
	// TypeKeywords maps a memory type to phrases that indicate it.
	TypeKeywords map[string][]string
	// HighImportance, DecisionSignals and LowImportance are phrases that
	// raise or lower importance.
	HighImportance  []string
	DecisionSignals []string
	LowImportance   []string
}

var (
	memoryTermsMu sync.RWMutex
	memoryTerms   = map[string]*MemoryTerms{}
)

// RegisterMemoryTerms adds or replaces the memory terms of a language.
func RegisterMemoryTerms(t *MemoryTerms) {
	memoryTermsMu.Lock()
	defer memoryTermsMu.Unlock()
	memoryTerms[t.Code] = t
}

// GetMemoryTerms returns the memory terms of a language, falling back to
// English.
func GetMemoryTerms(code string) *MemoryTerms {
	memoryTermsMu.RLock()
	defer memoryTermsMu.RUnlock()
	if t, ok := memoryTerms[code]; ok {
		return t
	}
	if t, ok := memoryTerms["en"]; ok {
		return t
	}
	return &MemoryTerms{Code: code, TypeKeywords: map[string][]string{}}
}

// vietnameseOnly are letters (lowercase) that occur in Vietnamese but not in
// English, French or German text: a cheap, reliable signal.
const vietnameseOnly = "ăâđêôơưạảấầẩẫậắằẳẵặẹẻẽếềểễệỉịọỏốồổỗộớờởỡợụủứừửữựỳỵỷỹ"

// DetectMemoryLanguage returns "vi" when the text carries Vietnamese-only
// letters, "zh" when it is mostly CJK, and "en" otherwise.
func DetectMemoryLanguage(text string) string {
	var vi, cjk, letters int
	for _, r := range strings.ToLower(text) {
		switch {
		case strings.ContainsRune(vietnameseOnly, r):
			vi++
			letters++
		case unicode.Is(unicode.Han, r):
			cjk++
			letters++
		case unicode.IsLetter(r):
			letters++
		}
	}
	switch {
	case letters == 0:
		return "en"
	case vi >= 2 || (vi == 1 && letters < 20):
		return "vi"
	case float64(cjk)/float64(letters) > 0.3:
		return "zh"
	default:
		return "en"
	}
}

func init() {
	RegisterMemoryTerms(&MemoryTerms{
		Code: "en",
		TypeKeywords: map[string][]string{
			"episodic":   {"happened", "occurred", "today", "yesterday", "earlier", "last week", "incident", "event"},
			"semantic":   {"is a", "is an", "means", "defined as", "refers to", "consists of", "is used", "typically"},
			"procedural": {"steps", "how to", "procedure", "process", "workflow", "first", "then", "next", "finally", "instructions", "run "},
			"decision":   {"decide", "decided", "chose", "chosen", "selected", "approved", "agreed", "resolution", "conclusion", "decision", "we will use"},
			"preference": {"prefer", "prefers", "preferred", "preference", "dislike", "dislikes", "favorite", "would rather", "opinion", "likes", "want", "wants"},
		},
		HighImportance:  []string{"critical", "important", "urgent", "mandatory", "required", "essential", "vital", "must", "never", "always"},
		DecisionSignals: []string{"decided", "approved", "agreed", "confirmed", "finalized", "resolved"},
		LowImportance:   []string{"maybe", "perhaps", "possibly", "might", "could be", "not sure", "unclear", "ambiguous"},
	})
	RegisterMemoryTerms(&MemoryTerms{
		Code: "vi",
		TypeKeywords: map[string][]string{
			"episodic":   {"đã xảy ra", "xảy ra", "hôm nay", "hôm qua", "tuần trước", "lúc nãy", "sự cố", "sự kiện"},
			"semantic":   {"là một", "nghĩa là", "được định nghĩa", "dùng để", "bao gồm", "thường", "khái niệm"},
			"procedural": {"các bước", "cách làm", "quy trình", "hướng dẫn", "đầu tiên", "sau đó", "tiếp theo", "cuối cùng", "chạy lệnh"},
			"decision":   {"quyết định", "đã chọn", "lựa chọn", "phê duyệt", "thống nhất", "đồng ý", "kết luận", "sẽ dùng"},
			"preference": {"thích", "ưu tiên", "không thích", "muốn", "ưa", "mong muốn", "quan điểm"},
		},
		HighImportance:  []string{"quan trọng", "nghiêm trọng", "khẩn cấp", "bắt buộc", "cần thiết", "thiết yếu", "phải", "không bao giờ", "luôn luôn"},
		DecisionSignals: []string{"quyết định", "phê duyệt", "đã thống nhất", "đã xác nhận", "đã chốt", "đã giải quyết"},
		LowImportance:   []string{"có lẽ", "có thể", "chắc là", "không chắc", "chưa rõ", "mơ hồ"},
	})
}
