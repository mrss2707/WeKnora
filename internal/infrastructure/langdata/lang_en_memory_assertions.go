package langdata

func init() {
	RegisterMemoryAssertionRules("en", &MemoryAssertionRules{
		Negations: map[string]bool{"not": true, "never": true, "no": true},
		Expansions: map[string]string{
			"cannot": "can not", "can't": "can not", "won't": "will not",
			"don't": "do not", "doesn't": "does not", "didn't": "did not",
			"isn't": "is not", "aren't": "are not", "wasn't": "was not",
			"weren't": "were not", "hasn't": "has not", "haven't": "have not",
			"shouldn't": "should not", "wouldn't": "would not", "couldn't": "could not",
		},
		Ambiguous: map[string]bool{
			"and": true, "or": true, "if": true, "unless": true, "because": true,
			"except": true, "only": true, "maybe": true, "perhaps": true,
			"sometimes": true, "probably": true, "might": true, "whether": true,
		},
	})
}
