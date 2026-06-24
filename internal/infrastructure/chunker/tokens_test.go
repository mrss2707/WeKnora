package chunker

import "testing"

func TestApproxTokenCount_English(t *testing.T) {
	got := ApproxTokenCount("The quick brown fox jumps over the lazy dog.", LangEnglish)
	// 44 chars / 4 ≈ 11 tokens
	if got < 9 || got > 13 {
		t.Errorf("English token estimate out of range: got %d, want 9..13", got)
	}
}

func TestApproxTokenCount_Chinese(t *testing.T) {
	got := ApproxTokenCount("这是一段中文测试内容用于检验分词估算", LangChinese)
	// 18 runes / 1.7 ≈ 10
	if got < 9 || got > 12 {
		t.Errorf("Chinese token estimate out of range: got %d, want 9..12", got)
	}
}

func TestApproxTokenCount_Empty(t *testing.T) {
	if got := ApproxTokenCount("", LangEnglish); got != 0 {
		t.Errorf("empty string should return 0 tokens, got %d", got)
	}
}

func TestApproxTokenCount_UnknownLang(t *testing.T) {
	got := ApproxTokenCount("Hello world hello world", "xx")
	if got <= 0 {
		t.Errorf("unknown lang should fall back to mixed, got %d", got)
	}
}

func TestDetectLanguage_English(t *testing.T) {
	if got := DetectLanguage("The quick brown fox jumps over the lazy dog."); got != LangEnglish {
		t.Errorf("expected English, got %s", got)
	}
}

func TestDetectLanguage_German(t *testing.T) {
	if got := DetectLanguage("Der schnelle braune Fuchs springt über den faulen Hund."); got != LangGerman {
		t.Errorf("expected German, got %s", got)
	}
}

func TestDetectLanguage_GermanByStopwords(t *testing.T) {
	// No umlauts but plenty of German function words.
	if got := DetectLanguage("Das ist ein Test und nicht mit Umlauten."); got != LangGerman {
		t.Errorf("expected German via stopwords, got %s", got)
	}
}

func TestDetectLanguage_Chinese(t *testing.T) {
	if got := DetectLanguage("这是一段中文测试内容"); got != LangChinese {
		t.Errorf("expected Chinese, got %s", got)
	}
}

func TestDetectLanguage_Mixed(t *testing.T) {
	got := DetectLanguage("This 这是 mixed 测试 content with 多语言 inside")
	if got != LangMixed {
		t.Errorf("expected Mixed, got %s", got)
	}
}

func TestCharsForTokenLimit_AppliesSafetyMargin(t *testing.T) {
	got := CharsForTokenLimit(1000, LangEnglish)
	// 1000 * 4 * 0.9 = 3600
	if got < 3500 || got > 3700 {
		t.Errorf("char budget for 1000 EN tokens out of range: got %d", got)
	}
}

func TestCharsForTokenLimit_ZeroTokens(t *testing.T) {
	if got := CharsForTokenLimit(0, LangEnglish); got != 0 {
		t.Errorf("zero tokens should give zero chars, got %d", got)
	}
}

func TestDetectLanguage_Vietnamese(t *testing.T) {
	// Vietnamese text with diacritics should be detected as Latin (not Mixed).
	got := DetectLanguage("Xin chào Việt Nam, đây là bài kiểm tra")
	if got == LangMixed {
		t.Errorf("Vietnamese text should not be LangMixed, got %s", got)
	}
	// It will be detected as English (Latin without German markers) — that's acceptable.
	if got != LangEnglish && got != LangVietnamese {
		t.Errorf("expected English or Vietnamese, got %s", got)
	}
}

func TestDetectLanguage_VietnameseNotMixed(t *testing.T) {
	// Pure Vietnamese with heavy diacritics must NOT be classified as Mixed.
	got := DetectLanguage("Người Việt Nam sử dụng tiếng Việt có dấu thanh")
	if got == LangMixed {
		t.Errorf("pure Vietnamese should not be LangMixed, got %s", got)
	}
}

func TestApproxTokenCount_Vietnamese(t *testing.T) {
	got := ApproxTokenCount("Xin chào Việt Nam", "vi")
	// 17 chars / 3.5 ≈ 5 tokens
	if got < 3 || got > 7 {
		t.Errorf("Vietnamese token estimate out of range: got %d, want 3..7", got)
	}
}

func TestCharsForTokenLimit_Vietnamese(t *testing.T) {
	got := CharsForTokenLimit(1000, "vi")
	// 1000 * 3.5 * 0.9 = 3150
	if got < 3000 || got > 3300 {
		t.Errorf("char budget for 1000 VI tokens out of range: got %d", got)
	}
}
