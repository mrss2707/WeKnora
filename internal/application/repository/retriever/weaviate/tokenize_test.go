package weaviate

import "testing"

func TestContainsCJK_Chinese(t *testing.T) {
	if !containsCJK("你好世界") {
		t.Error("Chinese text should contain CJK")
	}
}

func TestContainsCJK_Korean(t *testing.T) {
	if !containsCJK("안녕하세요") {
		t.Error("Korean text should contain CJK")
	}
}

func TestContainsCJK_Vietnamese(t *testing.T) {
	if containsCJK("Xin chào Việt Nam") {
		t.Error("Vietnamese text should NOT contain CJK")
	}
}

func TestContainsCJK_English(t *testing.T) {
	if containsCJK("hello world") {
		t.Error("English text should NOT contain CJK")
	}
}

func TestContainsCJK_Empty(t *testing.T) {
	if containsCJK("") {
		t.Error("Empty text should NOT contain CJK")
	}
}

func TestContainsCJK_Mixed(t *testing.T) {
	if !containsCJK("hello 你好 world") {
		t.Error("Mixed text with Chinese should contain CJK")
	}
}
