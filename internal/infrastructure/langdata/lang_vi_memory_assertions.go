package langdata

func init() {
	RegisterMemoryAssertionRules("vi", &MemoryAssertionRules{
		Negations:        map[string]bool{"không": true, "chẳng": true},
		LeadingAmbiguous: map[string]bool{"liệu": true},
		AmbiguousPhrases: []string{"có thể", "bao giờ"},
		Ambiguous: map[string]bool{
			"và": true, "hoặc": true, "nếu": true, "nhưng": true, "trừ": true,
			"chỉ": true, "chưa": true, "lẽ": true, "thỉnh": true,
		},
	})
}
