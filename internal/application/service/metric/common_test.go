package metric

import (
	"testing"
)

func TestSplitIntoWords_Vietnamese(t *testing.T) {
	tokens := splitIntoWords([]string{"người Việt"})
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens for 'người Việt', got %d: %v", len(tokens), tokens)
	}
	// Verify no characters are lost
	found := map[string]bool{}
	for _, tok := range tokens {
		found[tok] = true
	}
	if !found["người"] {
		t.Error("missing token 'người'")
	}
	if !found["Việt"] {
		t.Error("missing token 'Việt'")
	}
}

func TestSplitIntoWords_VietnameseDiacritics(t *testing.T) {
	tokens := splitIntoWords([]string{"được phép"})
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens for 'được phép', got %d: %v", len(tokens), tokens)
	}
	found := map[string]bool{}
	for _, tok := range tokens {
		found[tok] = true
	}
	if !found["được"] {
		t.Error("missing token 'được'")
	}
	if !found["phép"] {
		t.Error("missing token 'phép'")
	}
}

func TestSplitIntoWords_Chinese(t *testing.T) {
	tokens := splitIntoWords([]string{"你好世界"})
	if len(tokens) == 0 {
		t.Error("Chinese text produced no tokens")
	}
}

func TestSplitIntoWords_English(t *testing.T) {
	tokens := splitIntoWords([]string{"hello world test"})
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens for 'hello world test', got %d: %v", len(tokens), tokens)
	}
}

func TestSplitIntoWords_MixedVietnameseChinese(t *testing.T) {
	tokens := splitIntoWords([]string{"hello 你好 người"})
	if len(tokens) < 3 {
		t.Errorf("expected at least 3 tokens for mixed text, got %d: %v", len(tokens), tokens)
	}
}
