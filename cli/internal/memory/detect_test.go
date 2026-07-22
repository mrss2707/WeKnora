package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectPlatform_Cursor(t *testing.T) {
	dir := t.TempDir()
	// Create .cursorrules marker
	writeTestFile(t, filepath.Join(dir, ".cursorrules"), "")
	assert.Equal(t, "cursor", DetectPlatform(dir))
}

func TestDetectPlatform_CursorDir(t *testing.T) {
	dir := t.TempDir()
	// Create .cursor/ directory marker
	writeTestFile(t, filepath.Join(dir, ".cursor", "mcp.json"), "{}")
	assert.Equal(t, "cursor", DetectPlatform(dir))
}

func TestDetectPlatform_Copilot(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".vscode", "settings.json"), "{}")
	assert.Equal(t, "copilot", DetectPlatform(dir))
}

func TestDetectPlatform_CopilotGithub(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".github", "copilot-instructions.md"), "")
	assert.Equal(t, "copilot", DetectPlatform(dir))
}

func TestDetectPlatform_PaiCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "AGENTS.md"), "# Project")
	assert.Equal(t, "paicode", DetectPlatform(dir))
}

func TestDetectPlatform_ClaudeCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "CLAUDE.md"), "# Project")
	assert.Equal(t, "claude-code", DetectPlatform(dir))
}

func TestDetectPlatform_ClaudeCodeDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".claude", "settings.json"), "{}")
	assert.Equal(t, "claude-code", DetectPlatform(dir))
}

func TestDetectPlatform_Windsurf(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".windsurfrules"), "")
	assert.Equal(t, "windsurf", DetectPlatform(dir))
}

func TestDetectPlatform_WindsurfDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".windsurf", "hooks.json"), "{}")
	assert.Equal(t, "windsurf", DetectPlatform(dir))
}

func TestDetectPlatform_Cline(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".clinerules"), "")
	assert.Equal(t, "cline", DetectPlatform(dir))
}

func TestDetectPlatform_Continue(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".continue", "config.yaml"), "mcpServers:\n  - name: weknora")
	assert.Equal(t, "continue", DetectPlatform(dir))
}

func TestDetectPlatform_ContinueDir(t *testing.T) {
	dir := t.TempDir()
	// Just having .continue/ dir should detect
	writeTestFile(t, filepath.Join(dir, ".continue", "config.yaml"), "")
	assert.Equal(t, "continue", DetectPlatform(dir))
}

func TestDetectPlatform_Gemini(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "GEMINI.md"), "# Project")
	assert.Equal(t, "gemini", DetectPlatform(dir))
}

func TestDetectPlatform_GeminiDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".gemini", "settings.json"), "{}")
	assert.Equal(t, "gemini", DetectPlatform(dir))
}

func TestDetectPlatform_None(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "", DetectPlatform(dir))
}

func TestDetectPlatform_PriorityOrder(t *testing.T) {
	// Cursor should win over Copilot when both markers exist
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".cursorrules"), "")
	writeTestFile(t, filepath.Join(dir, ".vscode", "settings.json"), "{}")
	assert.Equal(t, "cursor", DetectPlatform(dir))

	// AGENTS.md should win over CLAUDE.md when both exist
	dir2 := t.TempDir()
	writeTestFile(t, filepath.Join(dir2, "AGENTS.md"), "# Project")
	writeTestFile(t, filepath.Join(dir2, "CLAUDE.md"), "# Project")
	assert.Equal(t, "paicode", DetectPlatform(dir2))
}

// writeTestFile creates a file with the given content, creating parent dirs as needed.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create dir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
