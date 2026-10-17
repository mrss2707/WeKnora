package langdata

import "testing"

func TestDetectMemoryLanguage(t *testing.T) {
	cases := map[string]string{
		"Kết nối postgres bị đầy pool khi tải cao":            "vi",
		"Quyết định dùng pgbouncer":                           "vi",
		"Postgres connection pool exhausted under heavy load": "en",
		"Café crème brûlée":                                   "en", // French accents are not Vietnamese
		"使用 PostgreSQL 作为主数据库":                                "zh",
		"":                                                    "en",
		"12345 !!!":                                           "en",
	}
	for text, want := range cases {
		if got := DetectMemoryLanguage(text); got != want {
			t.Errorf("DetectMemoryLanguage(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestGetMemoryTermsFallsBackToEnglish(t *testing.T) {
	if GetMemoryTerms("vi").Code != "vi" {
		t.Fatal("vi terms not registered")
	}
	if got := GetMemoryTerms("xx").Code; got != "en" {
		t.Fatalf("unknown language fallback = %q, want en", got)
	}
}
