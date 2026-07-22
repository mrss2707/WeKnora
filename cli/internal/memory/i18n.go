// Package memory provides i18n message catalog for CLI user-facing strings.
// Language selection via WEKNORA_LANGUAGE env var; defaults to vi-VN.
package memory

import "os"

// Locale is the currently active locale.
type Locale string

const (
	LocaleEnUS Locale = "en-US"
	LocaleViVN Locale = "vi-VN"
)

// CurrentLocale returns the active locale from WEKNORA_LANGUAGE env var.
func CurrentLocale() Locale {
	lang := os.Getenv("WEKNORA_LANGUAGE")
	switch lang {
	case "en-US", "en":
		return LocaleEnUS
	default:
		return LocaleViVN
	}
}

// T returns the translated message for the given key in the current locale.
func T(key string) string {
	locale := CurrentLocale()
	if locale == LocaleEnUS {
		if msg, ok := enUSMessages[key]; ok {
			return msg
		}
	}
	// Fallback to vi-VN
	if msg, ok := viVNMessages[key]; ok {
		return msg
	}
	return key
}

var enUSMessages = map[string]string{
	"setup.scanning":         "Scanning project directory...",
	"setup.kb_detected":      "Knowledge base detected: %s",
	"setup.no_kb":            "No knowledge base linked. Run `weknora link` first.",
	"setup.writing_mcp":      "Writing MCP config to %s",
	"setup.writing_hooks":    "Writing hook config to %s",
	"setup.writing_rules":    "Writing memory protocol rules to %s",
	"setup.done":             "Setup complete. Restart your agent to pick up changes.",
	"setup.dry_run":          "[DRY RUN] Would write to:",
	"setup.idempotent_mcp":   "MCP config already exists — skipping.",
	"setup.idempotent_hooks": "Hook config already exists — skipping.",
	"setup.idempotent_rules": "Memory protocol rules already present — skipping.",
	"setup.no_hooks":         "Platform does not support hooks — skipping.",
	"setup.creating_dir":     "Creating directory: %s",
	"setup.detected_platform": "Detected platform: %s",
	"setup.no_platform_detected": "No platform detected. Specify one with --platform.",
	"setup.unknown_platform": "Unknown platform: %s. Supported: claude-code, paicode, cursor, copilot, windsurf, cline, continue, gemini, auto.",
	"setup.server_url":       "Server URL:",
	"setup.api_key":          "API Key (optional):",
	"setup.mcp_server_path":  "MCP Server Path (optional):",
	"setup.mcp_server_desc":  "Path to main.py or custom script. Leave empty to use weknora CLI.",
	"setup.kb_select_title":  "Select Knowledge Bases",
	"setup.kb_select_desc":   "Space to select, Enter to confirm",
	"setup.kb_fetch_error":   "Could not connect to server. Enter KB IDs manually.",
	"setup.kb_manual_hint":   "Enter comma-separated KB IDs",
	"setup.connecting":       "Connecting to server...",
	"setup.kb_connected":     "Connected! %d knowledge base(s) found.",
	"hook.session_started":   "Session started with %d memories loaded.",
	"hook.session_skipped":   "Skipped: only %d memories (threshold: %d).",
	"hook.recall_topic":      "Recall topic: %s",
	"hook.no_kb_detected":    "No knowledge base detected for directory.",
	"hook.query_too_short":   "Query too short — skipping memory recall.",
	"hook.bug_classified":    "Classified as bug-fix → episodic/high.",
	"hook.arch_classified":   "Classified as architectural → decision/high.",
	"hook.activity_logged":   "Logged as activity only.",
}

var viVNMessages = map[string]string{
	"setup.scanning":         "Đang quét thư mục dự án...",
	"setup.kb_detected":      "Đã phát hiện knowledge base: %s",
	"setup.no_kb":            "Chưa liên kết knowledge base. Chạy `weknora link` trước.",
	"setup.writing_mcp":      "Đang ghi cấu hình MCP vào %s",
	"setup.writing_hooks":    "Đang ghi cấu hình hook vào %s",
	"setup.writing_rules":    "Đang ghi quy tắc memory protocol vào %s",
	"setup.done":             "Thiết lập hoàn tất. Khởi động lại agent để áp dụng.",
	"setup.dry_run":          "[DRY RUN] Sẽ ghi vào:",
	"setup.idempotent_mcp":   "Cấu hình MCP đã tồn tại — bỏ qua.",
	"setup.idempotent_hooks": "Cấu hình hook đã tồn tại — bỏ qua.",
	"setup.idempotent_rules": "Quy tắc memory protocol đã có — bỏ qua.",
	"setup.no_hooks":         "Nền tảng không hỗ trợ hooks — bỏ qua.",
	"setup.creating_dir":     "Đang tạo thư mục: %s",
	"setup.detected_platform": "Đã phát hiện nền tảng: %s",
	"setup.no_platform_detected": "Không phát hiện nền tảng. Chỉ định bằng --platform.",
	"setup.unknown_platform": "Nền tảng không xác định: %s. Hỗ trợ: claude-code, paicode, cursor, copilot, windsurf, cline, continue, gemini, auto.",
	"setup.server_url":       "URL máy chủ:",
	"setup.api_key":          "API Key (tùy chọn):",
	"setup.mcp_server_path":  "Đường dẫn MCP Server (tùy chọn):",
	"setup.mcp_server_desc":  "Đường dẫn đến main.py hoặc script tùy chỉnh. Để trống để dùng weknora CLI.",
	"setup.kb_select_title":  "Chọn Knowledge Base",
	"setup.kb_select_desc":   "Phím Space để chọn, Enter để xác nhận",
	"setup.kb_fetch_error":   "Không thể kết nối đến máy chủ. Nhập KB ID thủ công.",
	"setup.kb_manual_hint":   "Nhập KB ID cách nhau bằng dấu phẩy",
	"setup.connecting":       "Đang kết nối đến máy chủ...",
	"setup.kb_connected":     "Đã kết nối! Tìm thấy %d knowledge base.",
	"hook.session_started":   "Phiên bắt đầu với %d memories đã tải.",
	"hook.session_skipped":   "Bỏ qua: chỉ có %d memories (ngưỡng: %d).",
	"hook.recall_topic":      "Chủ đề recall: %s",
	"hook.no_kb_detected":    "Không phát hiện knowledge base cho thư mục.",
	"hook.query_too_short":   "Truy vấn quá ngắn — bỏ qua memory recall.",
	"hook.bug_classified":    "Phân loại: sửa lỗi → episodic/high.",
	"hook.arch_classified":   "Phân loại: kiến trúc → decision/high.",
	"hook.activity_logged":   "Đã ghi nhật ký hoạt động.",
}
