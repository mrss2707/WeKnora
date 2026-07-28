package memory_v2

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// packContext formats search results into structured XML with token budget metadata.
func (s *MemoryServiceV2Impl) packContext(ctx context.Context, query string, results []*types.MemorySearchResult) string {
	// Apply token budget
	adjusted, info := s.tokenBudget.Apply(ctx, results, s.config.TokenBudget)

	var b strings.Builder

	b.WriteString("<memory_context>\n")

	// Metadata
	b.WriteString(fmt.Sprintf("  <metadata query=%q>\n", query))
	b.WriteString(fmt.Sprintf("    <token_budget mode=%q used=\"%d\" remaining=\"%d\" />\n",
		info.Mode, info.Used, info.Remaining))
	b.WriteString(fmt.Sprintf("    <result_count>%d</result_count>\n", len(adjusted)))
	b.WriteString("  </metadata>\n")

	// Memories
	for i, r := range adjusted {
		if r.Memory == nil {
			continue
		}
		idx := i + 1
		b.WriteString(fmt.Sprintf("  <memory id=%q index=\"%d\">\n", r.Memory.ID, idx))
		b.WriteString(fmt.Sprintf("    <type>%s</type>\n", r.Memory.MemoryType))
		b.WriteString(fmt.Sprintf("    <verdict>%s</verdict>\n", r.Memory.Verdict))
		b.WriteString(fmt.Sprintf("    <importance>%d</importance>\n", r.Memory.Importance))
		b.WriteString(fmt.Sprintf("    <tier>%d</tier>\n", r.Memory.Tier))
		if r.Memory.SessionID != "" {
			b.WriteString(fmt.Sprintf("    <session_id>%s</session_id>\n", r.Memory.SessionID))
		}
		b.WriteString(fmt.Sprintf("    <score>%.4f</score>\n", r.Score))
		b.WriteString(fmt.Sprintf("    <content>%s</content>\n", escapeXML(r.Memory.Content)))
		if r.IsStale {
			b.WriteString(fmt.Sprintf("    <stale days=\"%d\" />\n", r.StaleDays))
		}
		b.WriteString("  </memory>\n")
	}

	b.WriteString("</memory_context>")
	return b.String()
}

// escapeXML escapes special characters for XML content.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
