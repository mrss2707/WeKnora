package langdata

import "regexp"

func init() {
	Register(&LanguageData{
		Code: "mixed",
		// Fallback: union of English and Chinese stopwords plus common German
		// particles. This is used when language detection returns "mixed" or
		// when an unknown language code is queried via Get().
		Stopwords: map[string]bool{
			// English (core set)
			"a": true, "an": true, "the": true, "is": true, "are": true,
			"was": true, "were": true, "be": true, "been": true, "being": true,
			"have": true, "has": true, "had": true, "do": true, "does": true,
			"did": true, "will": true, "would": true, "could": true, "should": true,
			"may": true, "might": true, "must": true, "can": true,
			"to": true, "of": true, "in": true, "for": true, "on": true,
			"with": true, "at": true, "by": true, "from": true, "as": true,
			"into": true, "through": true, "about": true,
			"and": true, "but": true, "or": true, "not": true, "no": true,
			"what": true, "how": true, "why": true, "when": true,
			"where": true, "which": true, "who": true, "whom": true,
			// Chinese (core set)
			"的": true, "了": true, "是": true, "在": true, "和": true,
			"与": true, "或": true, "不": true, "也": true, "都": true,
			"有": true, "这": true, "那": true, "就": true, "会": true,
			"我": true, "你": true, "他": true, "她": true, "它": true,
			"能": true, "要": true, "将": true, "已": true,
			// German (core set)
			"der": true, "die": true, "das": true, "und": true, "ist": true,
			"nicht": true, "mit": true, "auf": true, "ein": true, "eine": true,
			"für": true, "von": true, "sich": true, "den": true, "dem": true,
		},
		QuestionWords: []string{
			// English
			"what", "how", "why", "when", "where", "which", "who", "whom",
			// Chinese
			"什么", "如何", "怎么", "怎样", "为什么", "哪个", "哪些", "谁",
			// German
			"was", "wie", "warum", "wann", "wo", "welcher", "welche", "welches",
		},
		ChapterPatterns: []*regexp.Regexp{
			// English
			regexp.MustCompile(`(?m)^[ \t]*(?:Chapter|Section|Part)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`),
			// German
			regexp.MustCompile(`(?m)^[ \t]*(?:Kapitel|Abschnitt|Teil)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`),
			// Chinese
			regexp.MustCompile(`(?m)^[ \t]*第[ \t]*[一二三四五六七八九十百千零〇0-9]+[ \t]*(?:章|节|節|部分|篇)[ \t]?.{0,200}$`),
			// Vietnamese
			regexp.MustCompile(`(?m)^[ \t]*(?:Chương|Phần|Mục|Phụ lục)\s+(?:[0-9]+|[IVX]{1,5}|[A-Z])[\.: ].{0,200}$`),
			regexp.MustCompile(`(?m)^[ \t]*Chương\s+[0-9]{1,3}[ \t]*$`),
		},
		PageFooterPattern: regexp.MustCompile(
			`(?mi)^[ \t]*(?:Page|Seite|Trang|第?\s*页?\s*)\s*\d+(?:\s*(?:of|von|trên|\/|共)\s*\d+)?[ \t]*$`,
		),
		CharsPerToken: 3.0,
	})
}
