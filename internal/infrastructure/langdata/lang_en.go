package langdata

import "regexp"

func init() {
	Register(&LanguageData{
		Code: "en",
		Stopwords: map[string]bool{
			// Articles
			"a": true, "an": true, "the": true,
			// Be verbs
			"is": true, "are": true, "was": true, "were": true,
			"be": true, "been": true, "being": true, "am": true,
			// Auxiliary / modal verbs
			"have": true, "has": true, "had": true,
			"do": true, "does": true, "did": true,
			"will": true, "would": true, "could": true, "should": true,
			"may": true, "might": true, "must": true, "can": true, "shall": true,
			// Pronouns
			"i": true, "you": true, "he": true, "she": true, "it": true,
			"we": true, "they": true, "me": true, "him": true, "her": true,
			"us": true, "them": true, "my": true, "your": true, "his": true,
			"its": true, "our": true, "their": true,
			"myself": true, "yourself": true, "himself": true, "herself": true,
			"itself": true, "ourselves": true, "themselves": true,
			// Demonstratives
			"this": true, "that": true, "these": true, "those": true,
			// Prepositions
			"to": true, "of": true, "in": true, "for": true, "on": true,
			"with": true, "at": true, "by": true, "from": true, "as": true,
			"into": true, "through": true, "about": true, "above": true,
			"below": true, "between": true, "under": true, "over": true,
			"after": true, "before": true, "during": true, "without": true,
			"within": true, "along": true, "across": true, "behind": true,
			"beyond": true, "around": true, "against": true, "among": true,
			// Conjunctions
			"and": true, "but": true, "or": true, "nor": true, "yet": true,
			"so": true, "if": true, "because": true, "since": true, "while": true,
			"although": true, "unless": true, "until": true, "whether": true,
			// Relative / interrogative
			"what": true, "how": true, "why": true, "when": true,
			"where": true, "which": true, "who": true, "whom": true,
			"whose": true,
			// Common adverbs / particles
			"not": true, "no": true, "than": true, "too": true,
			"very": true, "just": true, "also": true, "only": true, "even": true,
			"now": true, "then": true, "here": true, "there": true,
		},
		QuestionWords: []string{
			"what", "what is", "what are", "what was", "what were",
			"how", "how do", "how does", "how did", "how can", "how to",
			"why", "why do", "why does", "why did", "why is",
			"when", "when do", "when did", "when is", "when was",
			"where", "where do", "where does", "where is", "where was",
			"which", "which is", "which are",
			"who", "who is", "who are", "who was",
			"whom", "whose",
			"can you", "could you", "would you", "please",
		},
		ChapterPatterns: []*regexp.Regexp{
			// Chapter 1, Section 2, Part III
			regexp.MustCompile(`(?m)^[ \t]*(?:Chapter|Section|Part)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`),
		},
		PageFooterPattern: regexp.MustCompile(`(?mi)^[ \t]*Page\s+\d+(?:\s*(?:of|\/)\s*\d+)?[ \t]*$`),
		CharsPerToken:     4.0,
	})
}
