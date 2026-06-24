package langdata

import "regexp"

func init() {
	Register(&LanguageData{
		Code: "de",
		Stopwords: map[string]bool{
			// Articles
			"der": true, "die": true, "das": true, "ein": true, "eine": true,
			"einer": true, "eines": true, "einem": true, "einen": true,
			// Pronouns
			"ich": true, "du": true, "er": true, "sie": true, "es": true,
			"wir": true, "ihr": true, "mich": true, "dich": true,
			"sich": true, "uns": true, "euch": true,
			"mein": true, "dein": true, "sein": true, "unser": true,
			// Prepositions
			"in": true, "an": true, "auf": true, "aus": true, "bei": true,
			"mit": true, "nach": true, "von": true, "zu": true, "für": true,
			"über": true, "unter": true, "vor": true, "hinter": true,
			"neben": true, "zwischen": true, "durch": true, "gegen": true,
			"ohne": true, "um": true, "bis": true,
			// Conjunctions
			"und": true, "oder": true, "aber": true, "denn": true, "sondern": true,
			"weil": true, "dass": true, "wenn": true, "als": true, "ob": true,
			"während": true, "bevor": true, "nachdem": true, "obwohl": true,
			// Verbs / auxiliaries
			"ist": true, "sind": true, "war": true, "hat": true, "haben": true,
			"werden": true, "kann": true, "muss": true, "soll": true, "will": true,
			"darf": true, "mag": true,
			// Common adverbs / particles
			"nicht": true, "auch": true, "noch": true, "schon": true, "sehr": true,
			"immer": true, "nur": true, "hier": true, "dort": true, "da": true,
			"wie": true, "was": true, "wer": true, "wo": true,
		},
		QuestionWords: []string{
			"was", "wer", "wen", "wem", "wessen",
			"wie", "warum", "weshalb", "wieso", "wofür",
			"wann", "wo", "wohin", "woher",
			"welcher", "welche", "welches",
			"wie viel", "wie viele",
			"können Sie", "könnten Sie", "bitte",
		},
		ChapterPatterns: []*regexp.Regexp{
			// Kapitel 1, Abschnitt 2, Teil III
			regexp.MustCompile(`(?m)^[ \t]*(?:Kapitel|Abschnitt|Teil)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`),
		},
		PageFooterPattern: regexp.MustCompile(`(?mi)^[ \t]*Seite\s+\d+(?:\s*(?:von|\/)\s*\d+)?[ \t]*$`),
		CharsPerToken:     4.5,
	})
}
