package langdata

import "regexp"

func init() {
	Register(&LanguageData{
		Code: "vi",
		Stopwords: map[string]bool{
			// Coordinating conjunctions
			"và": true, "hoặc": true, "hay": true, "nhưng": true, "mà": true,
			"vì": true, "nên": true, "nếu": true, "khi": true, "mặc dù": true,
			// Prepositions
			"của": true, "cho": true, "với": true, "từ": true, "đến": true,
			"tại": true, "ở": true, "trong": true, "ngoài": true, "trên": true,
			"dưới": true, "giữa": true, "sau": true, "trước": true,
			// Pronouns
			"tôi": true, "tao": true, "mình": true, "bạn": true, "anh": true,
			"chị": true, "em": true, "ông": true, "bà": true, "họ": true,
			"chúng": true, "nó": true,
			// Demonstratives
			"này": true, "đó": true, "ấy": true, "đây": true, "kia": true,
			// Common verbs / auxiliaries
			"là": true, "có": true, "được": true, "đã": true, "đang": true,
			"sẽ": true, "sắp": true, "phải": true, "cần": true,
			// Negation
			"không": true, "chưa": true, "chẳng": true,
			// Articles / determiners
			"một": true, "các": true, "những": true, "cái": true,
			// Common function words
			"rất": true, "quá": true, "lắm": true, "thì": true, "đi": true,
			"nào": true, "gì": true, "đâu": true, "sao": true, "thế": true,
			"như": true, "cùng": true,
			// Question words (included as stopwords since they carry little
			// semantic weight in keyword search)
			"cái gì": true, "thế nào": true, "như thế nào": true,
		},
		QuestionWords: []string{
			"cái gì", "thế nào", "như thế nào", "tại sao", "vì sao",
			"bao giờ", "khi nào", "ở đâu", "đâu", "ai", "mấy",
			"bao nhiêu", "có phải", "cái gì", "gì", "sao",
			"thế nào", "như nào", "ở đâu", "bao lâu",
		},
		ChapterPatterns: []*regexp.Regexp{
			// Chương 1, Phần II, Mục 3, Phụ lục A
			regexp.MustCompile(`(?m)^[ \t]*(?:Chương|Phần|Mục|Phụ lục)\s+(?:[0-9]+|[IVX]{1,5}|[A-Z])[\.: ].{0,200}$`),
			// Standalone "Chương N" without trailing colon
			regexp.MustCompile(`(?m)^[ \t]*Chương\s+[0-9]{1,3}[ \t]*$`),
		},
		PageFooterPattern: regexp.MustCompile(`(?mi)^[ \t]*Trang\s+\d+(?:\s*(?:trên|\/)\s*\d+)?[ \t]*$`),
		CharsPerToken:     3.5,
	})
}
