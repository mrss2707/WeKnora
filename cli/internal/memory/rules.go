package memory

import (
	"fmt"
	"strings"
)

const (
	rulesMarkerStart = "<!-- WEKNORA_MEMORY_PROTOCOL -->"
	rulesMarkerEnd   = "<!-- /WEKNORA_MEMORY_PROTOCOL -->"
)

// GenerateRules returns the memory protocol rules markdown block for the current locale.
// Multiple KB IDs are supported; each appears in a per-KB recall line.
// An empty slice emits a "No KBs linked yet" placeholder.
// The block is wrapped in HTML comment markers for idempotent injection.
func GenerateRules(kbIDs []string) string {
	locale := CurrentLocale()
	var content string
	switch locale {
	case LocaleEnUS:
		content = enUSRules(kbIDs)
	default:
		content = viVNRules(kbIDs)
	}
	return fmt.Sprintf("%s\n%s\n%s", rulesMarkerStart, strings.TrimSpace(content), rulesMarkerEnd)
}

// HasMemoryProtocolRules checks if the given file content already contains the memory protocol marker.
func HasMemoryProtocolRules(content string) bool {
	return strings.Contains(content, rulesMarkerStart)
}

// InjectRules appends the rules block to the file content. If the marker already exists,
// returns the content unchanged (idempotent).
func InjectRules(existingContent string, kbIDs []string) string {
	if HasMemoryProtocolRules(existingContent) {
		return existingContent
	}
	rules := GenerateRules(kbIDs)
	if existingContent == "" {
		return rules + "\n"
	}
	return strings.TrimRight(existingContent, "\n") + "\n\n" + rules + "\n"
}

func enUSRules(kbIDs []string) string {
	linkedKBs := "(none)"
	var recallLines string
	if len(kbIDs) > 0 {
		linkedKBs = strings.Join(kbIDs, ", ")
		var parts []string
		for _, id := range kbIDs {
			parts = append(parts, fmt.Sprintf("`memory_recall(kb_id=\"%s\", query=\"<2-4 keywords>\")`", id))
		}
		recallLines = strings.Join(parts, " and\n")
	} else {
		recallLines = "`memory_recall(kb_id=\"<your-kb-id>\", query=\"<2-4 keywords>\")`"
	}

	return fmt.Sprintf(`## WeKnora Memory Protocol

Linked KBs: %s

### Recall
At session start or when switching topics:
%s to load relevant context.
For complex questions: `+"`memory_recall(...)`"+` before researching.
Skip recall for: simple answers, basic commands, known facts.

### Save
After research with no matching memory → `+"`memory_save`"+` BEFORE answering.
Bug fixed → memory_type=episodic, importance=high (cause + solution).
Architectural decision → memory_type=decision, importance=high.
Tags: concept-based (auth, api, database). NO file names. Max 8 tags.

### Graph
Check for duplicates or contradictions: `+"`memory_graph(memory_id=\"<id>\")`"+`.
Before editing a memory: `+"`memory_graph(...)`"+` to see relationships.

### Status
Verify backend health: `+"`memory_status()`"+` at session start.
If unavailable → skip memory operations, report to user.`, linkedKBs, recallLines)
}

func viVNRules(kbIDs []string) string {
	linkedKBs := "(không có)"
	var recallLines string
	if len(kbIDs) > 0 {
		linkedKBs = strings.Join(kbIDs, ", ")
		var parts []string
		for _, id := range kbIDs {
			parts = append(parts, fmt.Sprintf("`memory_recall(kb_id=\"%s\", query=\"<2-4 từ khóa>\")`", id))
		}
		recallLines = strings.Join(parts, " và\n")
	} else {
		recallLines = "`memory_recall(kb_id=\"<your-kb-id>\", query=\"<2-4 từ khóa>\")`"
	}

	return fmt.Sprintf(`## WeKnora Memory Protocol

Linked KBs: %s

### Thu hồi (Recall)
Đầu phiên hoặc khi đổi chủ đề:
%s → tải context liên quan.
Với câu hỏi phức tạp: `+"`memory_recall(...)`"+` trước khi nghiên cứu.
Bỏ qua recall cho: câu trả lời đơn giản, lệnh cơ bản, sự kiện đã biết.

### Lưu (Save)
Sau khi nghiên cứu không có memory khớp → `+"`memory_save`"+` TRƯỚC KHI trả lời.
Bug đã sửa → memory_type=episodic, importance=high (nguyên nhân + giải pháp).
Quyết định kiến trúc → memory_type=decision, importance=high.
Tags: dựa trên khái niệm (auth, api, database). KHÔNG dùng tên file. Tối đa 8 tags.

### Đồ thị (Graph)
Kiểm tra trùng lặp hoặc mâu thuẫn: `+"`memory_graph(memory_id=\"<id>\")`"+`.
Trước khi sửa memory: `+"`memory_graph(...)`"+` để xem quan hệ.

### Trạng thái (Status)
Xác minh backend: `+"`memory_status()`"+` đầu phiên.
Nếu không khả dụng → bỏ qua thao tác memory, báo cho người dùng.`, linkedKBs, recallLines)
}
