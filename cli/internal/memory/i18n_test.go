package memory

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCurrentLocaleDefault(t *testing.T) {
	os.Unsetenv("WEKNORA_LANGUAGE")
	assert.Equal(t, LocaleViVN, CurrentLocale())
}

func TestCurrentLocaleEnUS(t *testing.T) {
	os.Setenv("WEKNORA_LANGUAGE", "en-US")
	defer os.Unsetenv("WEKNORA_LANGUAGE")
	assert.Equal(t, LocaleEnUS, CurrentLocale())
}

func TestCurrentLocaleEn(t *testing.T) {
	os.Setenv("WEKNORA_LANGUAGE", "en")
	defer os.Unsetenv("WEKNORA_LANGUAGE")
	assert.Equal(t, LocaleEnUS, CurrentLocale())
}

func TestTViVN(t *testing.T) {
	os.Unsetenv("WEKNORA_LANGUAGE")
	msg := T("setup.scanning")
	assert.Equal(t, "Đang quét thư mục dự án...", msg)
}

func TestTEnUS(t *testing.T) {
	os.Setenv("WEKNORA_LANGUAGE", "en-US")
	defer os.Unsetenv("WEKNORA_LANGUAGE")
	msg := T("setup.scanning")
	assert.Equal(t, "Scanning project directory...", msg)
}

func TestTFallback(t *testing.T) {
	os.Setenv("WEKNORA_LANGUAGE", "en-US")
	defer os.Unsetenv("WEKNORA_LANGUAGE")
	// Key exists in vi-VN but not in en-US → fallback to vi-VN
	msg := T("hook.bug_classified")
	assert.Equal(t, "Classified as bug-fix → episodic/high.", msg)
}

func TestTMissingKey(t *testing.T) {
	msg := T("nonexistent.key")
	assert.Equal(t, "nonexistent.key", msg)
}
