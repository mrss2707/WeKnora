package types

import "testing"

// develop keeps Vietnamese a first-class locale; main's supportedLocales
// (locale.go) must always carry it or embed/IM default locales silently drop it.
func TestVietnameseLocaleIsSupported(t *testing.T) {
	if got := NormalizeSupportedLocale("vi-VN"); got != "vi-VN" {
		t.Errorf("NormalizeSupportedLocale(vi-VN) = %q", got)
	}
	if got := NormalizeEmbedDefaultLocale(" vi-VN "); got != "vi-VN" {
		t.Errorf("NormalizeEmbedDefaultLocale(vi-VN) = %q", got)
	}
}
