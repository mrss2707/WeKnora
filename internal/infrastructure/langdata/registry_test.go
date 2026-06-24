package langdata

import (
	"testing"
)

func TestGetVietnamese(t *testing.T) {
	data := Get("vi")
	if data == nil {
		t.Fatal("Get(\"vi\") returned nil")
	}
	if data.Code != "vi" {
		t.Errorf("expected Code \"vi\", got %q", data.Code)
	}

	// Stopwords must contain key Vietnamese stopwords
	for _, sw := range []string{"và", "của", "là", "có", "được", "cho", "với", "này", "đó", "từ", "những", "các", "một", "đã", "đang", "sẽ", "không", "hay", "hoặc", "nhưng", "vì", "nếu", "khi"} {
		if !data.Stopwords[sw] {
			t.Errorf("Vietnamese stopwords missing %q", sw)
		}
	}

	// Question words must contain Vietnamese question words
	qwSet := make(map[string]bool)
	for _, qw := range data.QuestionWords {
		qwSet[qw] = true
	}
	for _, qw := range []string{"tại sao", "bao giờ", "ai", "bao nhiêu", "ở đâu"} {
		if !qwSet[qw] {
			t.Errorf("Vietnamese question words missing %q", qw)
		}
	}

	// ChapterPatterns must be non-empty
	if len(data.ChapterPatterns) == 0 {
		t.Error("Vietnamese ChapterPatterns is empty")
	}

	// CharsPerToken
	if data.CharsPerToken != 3.5 {
		t.Errorf("expected CharsPerToken 3.5, got %f", data.CharsPerToken)
	}
}

func TestGetChinese(t *testing.T) {
	data := Get("zh")
	if data == nil {
		t.Fatal("Get(\"zh\") returned nil")
	}
	if data.Code != "zh" {
		t.Errorf("expected Code \"zh\", got %q", data.Code)
	}
	for _, sw := range []string{"的", "了", "是", "在", "和", "不", "也", "都"} {
		if !data.Stopwords[sw] {
			t.Errorf("Chinese stopwords missing %q", sw)
		}
	}
	if data.CharsPerToken != 1.7 {
		t.Errorf("expected CharsPerToken 1.7, got %f", data.CharsPerToken)
	}
}

func TestGetEnglish(t *testing.T) {
	data := Get("en")
	if data == nil {
		t.Fatal("Get(\"en\") returned nil")
	}
	for _, sw := range []string{"the", "is", "at", "which", "on", "and", "but", "or"} {
		if !data.Stopwords[sw] {
			t.Errorf("English stopwords missing %q", sw)
		}
	}
	if data.CharsPerToken != 4.0 {
		t.Errorf("expected CharsPerToken 4.0, got %f", data.CharsPerToken)
	}
}

func TestGetUnknownReturnsFallback(t *testing.T) {
	data := Get("unknown_lang_xyz")
	if data == nil {
		t.Fatal("Get(\"unknown_lang_xyz\") returned nil")
	}
	// Should return mixed defaults (registered by lang_mixed.go init)
	if data.CharsPerToken != 3.0 {
		t.Errorf("expected fallback CharsPerToken 3.0, got %f", data.CharsPerToken)
	}
	// Mixed has stopwords
	if len(data.Stopwords) == 0 {
		t.Error("fallback Stopwords is empty")
	}
}

func TestVietnameseChapterPatterns(t *testing.T) {
	data := Get("vi")
	// Verify at least one pattern matches a Vietnamese chapter heading
	matched := false
	for _, pat := range data.ChapterPatterns {
		if pat.MatchString("Chương 1: Giới thiệu") {
			matched = true
			break
		}
	}
	if !matched {
		t.Error("no Vietnamese ChapterPattern matched \"Chương 1: Giới thiệu\"")
	}
}

func TestVietnamesePageFooterPattern(t *testing.T) {
	data := Get("vi")
	if data.PageFooterPattern == nil {
		t.Fatal("Vietnamese PageFooterPattern is nil")
	}
	if !data.PageFooterPattern.MatchString("Trang 5") {
		t.Error("PageFooterPattern did not match \"Trang 5\"")
	}
}
