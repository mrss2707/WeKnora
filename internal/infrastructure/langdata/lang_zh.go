package langdata

import "regexp"

func init() {
	Register(&LanguageData{
		Code: "zh",
		Stopwords: map[string]bool{
			// Functional words
			"的": true, "了": true, "是": true, "在": true, "和": true,
			"与": true, "或": true, "不": true, "也": true, "都": true,
			"有": true, "这": true, "那": true, "就": true, "会": true,
			// Pronouns and demonstratives
			"我": true, "你": true, "他": true, "她": true, "它": true,
			"我们": true, "他们": true, "它们": true, "自己": true,
			// Prepositions and conjunctions
			"从": true, "对": true, "把": true, "被": true,
			"让": true, "给": true, "用": true, "以": true, "为": true,
			"向": true, "到": true, "过": true, "之": true, "而": true,
			"但": true, "如果": true, "虽然": true, "因为": true, "所以": true,
			// Auxiliaries and aspect markers
			"能": true, "可以": true, "要": true, "将": true, "已": true,
			"正": true, "着": true,
			// Question / discourse
			"什么": true, "怎么": true, "怎样": true, "如何": true,
			"为什么": true, "哪个": true, "哪些": true, "谁": true,
			// Common particles
			"吗": true, "呢": true, "吧": true, "啊": true, "呀": true,
			"嘛": true,
		},
		QuestionWords: []string{
			"什么是", "什么", "如何", "怎么", "怎样", "为什么", "为何",
			"哪个", "哪些", "谁", "何时", "何地", "请问", "请告诉我",
			"帮我", "我想知道", "我想了解", "多少", "几", "哪里", "哪儿",
		},
		ChapterPatterns: []*regexp.Regexp{
			// 第一章, 第3节, 第 1 部分
			regexp.MustCompile(`(?m)^[ \t]*第[ \t]*[一二三四五六七八九十百千零〇0-9]+[ \t]*(?:章|节|節|部分|篇)[ \t]?.{0,200}$`),
		},
		PageFooterPattern: regexp.MustCompile(`(?mi)^[ \t]*第?\s*\d+\s*页\s*(?:[\/共]\s*\d+\s*页)?[ \t]*$`),
		CharsPerToken:     1.7,
	})
}
