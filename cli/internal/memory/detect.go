package memory

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectPlatform scans the given directory for platform marker files and returns
// the most likely platform identifier. Returns "" if no platform is detected.
//
// Priority order follows SaiMem's init-detector.ts detectAllPlatforms pattern:
//  1. .cursor/ or .cursorrules → "cursor"
//  2. .vscode/ or .github/ → "copilot"
//  3. AGENTS.md or SETTINGS.md → "paicode"
//  4. CLAUDE.md or .claude/ → "claude-code"
//  5. .windsurfrules or .windsurf/ → "windsurf"
//  6. .clinerules → "cline"
//  7. .continue/ or config.yaml with mcpServers.*weknora → "continue"
//  8. GEMINI.md or .gemini/ → "gemini"
func DetectPlatform(cwd string) string {
	// Priority-ordered detectors
	detectors := []struct {
		name string
		fn   func(string) bool
	}{
		{"cursor", detectCursor},
		{"copilot", detectCopilot},
		{"paicode", detectPaiCode},
		{"claude-code", detectClaudeCode},
		{"windsurf", detectWindsurf},
		{"cline", detectCline},
		{"continue", detectContinue},
		{"gemini", detectGemini},
	}

	for _, d := range detectors {
		if d.fn(cwd) {
			return d.name
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func detectCursor(cwd string) bool {
	return dirExists(filepath.Join(cwd, ".cursor")) || fileExists(filepath.Join(cwd, ".cursorrules"))
}

func detectCopilot(cwd string) bool {
	return dirExists(filepath.Join(cwd, ".vscode")) || dirExists(filepath.Join(cwd, ".github"))
}

func detectPaiCode(cwd string) bool {
	return fileExists(filepath.Join(cwd, "AGENTS.md")) || fileExists(filepath.Join(cwd, "SETTINGS.md"))
}

func detectClaudeCode(cwd string) bool {
	return fileExists(filepath.Join(cwd, "CLAUDE.md")) || dirExists(filepath.Join(cwd, ".claude"))
}

func detectWindsurf(cwd string) bool {
	return fileExists(filepath.Join(cwd, ".windsurfrules")) || dirExists(filepath.Join(cwd, ".windsurf"))
}

func detectCline(cwd string) bool {
	return fileExists(filepath.Join(cwd, ".clinerules"))
}

func detectContinue(cwd string) bool {
	return dirExists(filepath.Join(cwd, ".continue")) || detectContinueYAML(cwd)
}

// detectContinueYAML checks if .continue/config.yaml exists and contains mcpServers with weknora.
func detectContinueYAML(cwd string) bool {
	path := filepath.Join(cwd, ".continue", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "mcpServers") && strings.Contains(string(data), "weknora")
}

func detectGemini(cwd string) bool {
	return fileExists(filepath.Join(cwd, "GEMINI.md")) || dirExists(filepath.Join(cwd, ".gemini"))
}
