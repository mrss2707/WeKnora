package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	sdk "github.com/Tencent/WeKnora/client"
)

// ---------------------------------------------------------------------------
// Hook input/output types
// ---------------------------------------------------------------------------

// HookStdinBase is the common fields all hook stdin payloads carry.
type HookStdinBase struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

// SessionStartInput is the stdin payload for the session-start hook.
type SessionStartInput struct {
	HookStdinBase
}

// SessionStartOutput is the stdout payload for the session-start hook.
type SessionStartOutput struct {
	HookSpecificOutput *SessionStartHookOutput `json:"hookSpecificOutput,omitempty"`
}

// SessionStartHookOutput carries PaiCode/Claude-compatible SessionStart context.
type SessionStartHookOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// PromptContextInput is the stdin payload for the prompt-context hook.
type PromptContextInput struct {
	HookStdinBase
	UserMessage string              `json:"user_message"`
	Transcript  []map[string]string `json:"transcript,omitempty"`
}

// PromptContextOutput is the stdout payload for the prompt-context hook.
type PromptContextOutput struct {
	SystemMessage string `json:"systemMessage,omitempty"`
}

// PostToolInput is the stdin payload for the post-tool hook.
type PostToolInput struct {
	HookStdinBase
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// PostToolOutput is the stdout payload for the post-tool hook.
type PostToolOutput struct {
	Continue bool `json:"continue"`
}

// PreCompactInput is the stdin payload for the pre-compact hook.
type PreCompactInput struct {
	HookStdinBase
	Transcript []map[string]string `json:"transcript"`
}

// PreCompactOutput is the stdout payload for the pre-compact hook.
type PreCompactOutput struct {
	Continue bool `json:"continue"`
}

// SessionEndInput is the stdin payload for the session-end hook.
type SessionEndInput struct {
	HookStdinBase
}

// SessionEndOutput is the stdout payload for the session-end hook.
type SessionEndOutput struct{}

// ---------------------------------------------------------------------------
// Shared state (per-process, lives as long as CLI invocation)
// ---------------------------------------------------------------------------

var (
	// sessionSeenUUIDs tracks which memory UUIDs have been returned this session.
	sessionSeenUUIDs = make(map[string]map[string]bool) // session_id → set of UUIDs

	// activityLog accumulates simple activity entries during a session.
	activityLog = make(map[string][]activityEntry) // session_id → entries
)

type activityEntry struct {
	ToolName  string `json:"tool_name"`
	Summary   string `json:"summary"`
	Timestamp string `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Hook runner
// ---------------------------------------------------------------------------

// HookService bundles the SDK methods hooks need.
type HookService interface {
	SearchMemories(ctx context.Context, kbID, query string, limit int, memoryType, sessionID string, minScore float64) ([]sdk.MemorySearchResult, error)
	CreateMemory(ctx context.Context, req *sdk.CreateMemoryRequest) (*sdk.SaveMemoryResult, error)
	GetMemoryStatus(ctx context.Context) (*sdk.MemoryStatusResult, error)
	ListKnowledgeBases(ctx context.Context) ([]sdk.KnowledgeBase, error)
}

// HookContext holds shared dependencies for hook handlers.
type HookContext struct {
	Client HookService
	Cache  CacheStore
	KBID   string // resolved from CWD or WEKNORA_KB_ID
}

// RunHook dispatches to the correct handler based on the event name.
// It reads stdin JSON, processes, and writes stdout JSON.
func RunHook(event string, hctx *HookContext) error {
	switch event {
	case "session-start":
		return runSessionStart(hctx)
	case "post-tool":
		return runPostTool(hctx)
	default:
		return fmt.Errorf("unknown hook event: %s", event)
	}
}

// ---------------------------------------------------------------------------
// 5a. session-start
// ---------------------------------------------------------------------------

func runSessionStart(hctx *HookContext) error {
	var in SessionStartInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	out := SessionStartOutput{}

	// Detect KB from cwd or env
	kbID := resolveKBID(hctx, in.CWD)
	if kbID == "" {
		return writeJSON(os.Stdout, out)
	}

	// Count memories
	status, err := hctx.Client.GetMemoryStatus(context.Background())
	if err != nil || !status.Available {
		return writeJSON(os.Stdout, out)
	}

	// Skip if ≤5 memories
	if status.MemoryCount <= 5 {
		fmt.Fprintf(os.Stderr, T("hook.session_skipped")+"\n", status.MemoryCount, 5)
		return writeJSON(os.Stdout, out)
	}

	// Search for recent memories to derive a recall topic
	results, err := hctx.Client.SearchMemories(context.Background(), kbID, "recent session context", 5, "", "", 0.1)
	if err != nil || len(results) == 0 {
		fmt.Fprintf(os.Stderr, "%s\n%d\n", T("hook.session_started"), status.MemoryCount)
		return writeJSON(os.Stdout, out)
	}

	// Build compact XML context
	out.HookSpecificOutput = &SessionStartHookOutput{
		HookEventName:     "SessionStart",
		AdditionalContext: buildCompactXML(results, 1500),
	}
	fmt.Fprintf(os.Stderr, "%s\n%d\n", T("hook.session_started"), status.MemoryCount)
	return writeJSON(os.Stdout, out)
}

// ---------------------------------------------------------------------------
// 5b. prompt-context (most complex — 15 skip gates)
// ---------------------------------------------------------------------------

func runPromptContext(hctx *HookContext) error {
	var in PromptContextInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	out := PromptContextOutput{}

	kbID := resolveKBID(hctx, in.CWD)
	query := strings.TrimSpace(in.UserMessage)

	// Gate 1: No KB detected
	if kbID == "" {
		return writeJSON(os.Stdout, out)
	}

	// Gate 2: Query too short
	if len(query) < 5 {
		return writeJSON(os.Stdout, out)
	}

	// Gate 3: < 2 meaningful terms
	if countMeaningfulTerms(query) < 2 {
		return writeJSON(os.Stdout, out)
	}

	// Gate 4: Cache hit (60s TTL)
	cacheKey := "hook:prompt:" + kbID + ":" + query
	if cached, ok := hctx.Cache.Get(cacheKey); ok {
		if s, ok := cached.(string); ok {
			out.SystemMessage = s
			fmt.Fprintf(os.Stderr, "%s\n", T("hook.cache_hit"))
			return writeJSON(os.Stdout, out)
		}
	}

	// Gate 5: Session dedup
	if isSessionDedup(in.SessionID, query) {
		return writeJSON(os.Stdout, out)
	}

	// Unified search (1 API call, not 3)
	results, err := hctx.Client.SearchMemories(context.Background(), kbID, query, 20, "", "", 0.10)
	if err != nil {
		return writeJSON(os.Stdout, out)
	}

	// Gate 6: Cosine floor gate
	if len(results) == 0 || maxScore(results) < 0.25 {
		fmt.Fprintf(os.Stderr, "%s\n", T("hook.below_threshold"))
		return writeJSON(os.Stdout, out)
	}

	// Gate 7: Topic overlap filter
	if !hasTopicOverlap(query, results) {
		return writeJSON(os.Stdout, out)
	}

	// Sort: critical-first, then score-descending
	sortResults(results)

	// Cap at 6 results
	if len(results) > 6 {
		results = results[:6]
	}

	// Format compact XML
	xml := buildCompactXML(results, 2000)

	// Cache result (60s TTL), persist session dedup (30min TTL)
	hctx.Cache.Set(cacheKey, xml, 60*time.Second)
	cacheSessionDedup(in.SessionID, query)
	addSessionUUIDs(in.SessionID, results)

	out.SystemMessage = xml
	return writeJSON(os.Stdout, out)
}

// ---------------------------------------------------------------------------
// 5c. post-tool
// ---------------------------------------------------------------------------

func runPostTool(hctx *HookContext) error {
	var in PostToolInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	out := PostToolOutput{Continue: true}

	// Filter: only Edit/Write/NotebookEdit tools
	if !isRelevantTool(in.ToolName) {
		return writeJSON(os.Stdout, out)
	}

	kbID := resolveKBID(hctx, in.CWD)
	if kbID == "" {
		return writeJSON(os.Stdout, out)
	}

	// Classify: bug-fix → episodic/high; arch → decision/high
	toolInputStr := string(in.ToolInput)
	memType, importance := classifyToolAction(in.ToolName, toolInputStr)

	if memType != "" {
		// Save as memory
		summary := buildToolSummary(in.ToolName, toolInputStr)
		_, err := hctx.Client.CreateMemory(context.Background(), &sdk.CreateMemoryRequest{
			KbID:       kbID,
			Content:    summary,
			MemoryType: memType,
			Importance: importance,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "memory save error: %v\n", err)
		}
	} else {
		// Activity log only
		logActivity(in.SessionID, in.ToolName, toolInputStr)
		fmt.Fprintf(os.Stderr, "%s\n", T("hook.activity_logged"))
	}

	return writeJSON(os.Stdout, out)
}

// ---------------------------------------------------------------------------
// 5d. pre-compact
// ---------------------------------------------------------------------------

func runPreCompact(hctx *HookContext) error {
	var in PreCompactInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	out := PreCompactOutput{Continue: true}

	kbID := resolveKBID(hctx, in.CWD)
	if kbID == "" {
		return writeJSON(os.Stdout, out)
	}

	// Count assistant messages
	assistantCount := 0
	for _, msg := range in.Transcript {
		if role, ok := msg["role"]; ok && role == "assistant" {
			assistantCount++
		}
	}

	// Skip if < 5 assistant messages
	if assistantCount < 5 {
		fmt.Fprintf(os.Stderr, T("hook.compact_skipped")+"\n", assistantCount, 5)
		return writeJSON(os.Stdout, out)
	}

	// Scan for decision/fix signals
	snippets := extractSignalSnippets(in.Transcript)
	saved := 0
	for _, s := range snippets {
		_, err := hctx.Client.CreateMemory(context.Background(), &sdk.CreateMemoryRequest{
			KbID:       kbID,
			Content:    s.content,
			MemoryType: s.memType,
			Importance: s.importance,
			Tags:       []string{"pre-compact"},
		})
		if err == nil {
			saved++
		}
	}

	fmt.Fprintf(os.Stderr, T("hook.compact_saved")+"\n", saved)
	return writeJSON(os.Stdout, out)
}

// ---------------------------------------------------------------------------
// 5e. session-end
// ---------------------------------------------------------------------------

func runSessionEnd(hctx *HookContext) error {
	var in SessionEndInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	out := SessionEndOutput{}

	kbID := resolveKBID(hctx, in.CWD)
	if kbID == "" {
		return writeJSON(os.Stdout, out)
	}

	// Flush activity log
	entries := activityLog[in.SessionID]
	if len(entries) > 0 {
		summary := buildActivitySummary(entries)
		hctx.Client.CreateMemory(context.Background(), &sdk.CreateMemoryRequest{
			KbID:       kbID,
			Content:    summary,
			MemoryType: "procedural",
			Tags:       []string{"session-summary"},
		})
		delete(activityLog, in.SessionID)
		fmt.Fprintf(os.Stderr, T("hook.end_flushed")+"\n", len(entries))
	}

	// Clean up session dedup
	delete(sessionSeenUUIDs, in.SessionID)

	return writeJSON(os.Stdout, out)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveKBID gets the KB ID from env or from linked project.
func resolveKBID(hctx *HookContext, cwd string) string {
	if hctx.KBID != "" {
		return hctx.KBID
	}
	if kbID := os.Getenv("WEKNORA_KB_ID"); kbID != "" {
		return kbID
	}
	// Try to detect from project link
	// For now, just use env var
	return ""
}

// countMeaningfulTerms counts terms that are "meaningful" (not stopwords, >2 chars).
// CamelCase and dotted names count as 1 term each.
func countMeaningfulTerms(query string) int {
	// Split on whitespace, punctuation
	parts := strings.Fields(query)
	count := 0
	for _, p := range parts {
		p = strings.Trim(p, ".,;:!?\"'()[]{}")
		if len(p) >= 3 {
			count++
		} else if hasCamelOrDots(p) {
			count++
		}
	}
	return count
}

func hasCamelOrDots(s string) bool {
	if strings.Contains(s, ".") && len(s) >= 4 {
		return true
	}
	hasUpper := false
	hasLower := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
		if r >= 'a' && r <= 'z' {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

func maxScore(results []sdk.MemorySearchResult) float64 {
	max := 0.0
	for _, r := range results {
		if r.Score > max {
			max = r.Score
		}
	}
	return max
}

// hasTopicOverlap checks if at least 1 shared term exists between query and
// any memory's tags.
func hasTopicOverlap(query string, results []sdk.MemorySearchResult) bool {
	queryTerms := extractTerms(query)
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		for _, tag := range r.Memory.Tags {
			for _, qt := range queryTerms {
				if strings.Contains(strings.ToLower(tag), strings.ToLower(qt)) ||
					strings.Contains(strings.ToLower(qt), strings.ToLower(tag)) {
					return true
				}
			}
		}
	}
	return false
}

func extractTerms(s string) []string {
	parts := strings.Fields(strings.ToLower(s))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, ".,;:!?\"'()[]{}")
		if len(p) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// sortResults sorts critical-first (importance >= 2), then score-descending.
func sortResults(results []sdk.MemorySearchResult) {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			iCrit := results[i].Memory != nil && results[i].Memory.Importance >= 2
			jCrit := results[j].Memory != nil && results[j].Memory.Importance >= 2
			if iCrit != jCrit {
				if jCrit {
					results[i], results[j] = results[j], results[i]
				}
			} else if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// buildCompactXML builds the compact XML index format for memory results.
func buildCompactXML(results []sdk.MemorySearchResult, tokenBudget int) string {
	var b strings.Builder
	b.WriteString(`<weknora_index hint="Call memory_detail(id) on interesting results">` + "\n")
	tokens := 0
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		preview := r.Memory.Content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		tagStr := ""
		if len(r.Memory.Tags) > 0 {
			tagStr = strings.Join(r.Memory.Tags, ",")
		}
		line := fmt.Sprintf(`  <m id="%s" type="%s" imp="%d" score="%.2f" tags="%s">%s</m>`+"\n",
			r.Memory.ID, r.Memory.MemoryType, r.Memory.Importance, r.Score, tagStr, preview)
		lineTokens := estimateTokens(line)
		if tokens+lineTokens > tokenBudget {
			break
		}
		b.WriteString(line)
		tokens += lineTokens
	}
	b.WriteString("</weknora_index>")
	return b.String()
}

// estimateTokens approximates token count (4 chars / token).
func estimateTokens(s string) int {
	return len(s) / 4
}

// writeJSON encodes v as JSON and writes it to w followed by a newline.
func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func isSessionDedup(sessionID, query string) bool {
	uuids, ok := sessionSeenUUIDs[sessionID]
	if !ok {
		return false
	}
	// Simple dedup by query prefix
	prefix := query
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	for uuid := range uuids {
		if strings.Contains(uuid, prefix) {
			return true
		}
	}
	return false
}

func cacheSessionDedup(sessionID, query string) {
	if len(query) > 30 {
		query = query[:30]
	}
	if sessionSeenUUIDs[sessionID] == nil {
		sessionSeenUUIDs[sessionID] = make(map[string]bool)
	}
	sessionSeenUUIDs[sessionID]["query:"+query] = true
}

func addSessionUUIDs(sessionID string, results []sdk.MemorySearchResult) {
	for _, r := range results {
		if r.Memory != nil {
			if sessionSeenUUIDs[sessionID] == nil {
				sessionSeenUUIDs[sessionID] = make(map[string]bool)
			}
			sessionSeenUUIDs[sessionID][r.Memory.ID] = true
		}
	}
}

func isRelevantTool(name string) bool {
	switch name {
	case "Edit", "Write", "NotebookEdit", "Bash", "Read":
		return true
	}
	return false
}

// classifyToolAction classifies a tool action as bug-fix, architecture, or log-only.
func classifyToolAction(name, input string) (memType string, importance int) {
	lower := strings.ToLower(input)

	// Bug-fix signals
	bugKeywords := []string{"fix", "bug", "error", "lỗi", "sửa", "patch", "hotfix", "workaround"}
	for _, kw := range bugKeywords {
		if strings.Contains(lower, kw) {
			fmt.Fprintf(os.Stderr, "%s\n", T("hook.bug_classified"))
			return "episodic", 2
		}
	}

	// Architecture signals
	archKeywords := []string{"architecture", "design", "interface", "pattern", "refactor", "api", "schema", "migration"}
	for _, kw := range archKeywords {
		if strings.Contains(lower, kw) {
			fmt.Fprintf(os.Stderr, "%s\n", T("hook.arch_classified"))
			return "decision", 2
		}
	}

	return "", 0
}

func buildToolSummary(name, input string) string {
	preview := input
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("[%s] %s", name, preview)
}

func logActivity(sessionID, toolName, input string) {
	summary := buildToolSummary(toolName, input)
	activityLog[sessionID] = append(activityLog[sessionID], activityEntry{
		ToolName:  toolName,
		Summary:   summary,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

type snippetResult struct {
	content    string
	memType    string
	importance int
}

func extractSignalSnippets(transcript []map[string]string) []snippetResult {
	var out []snippetResult
	signals := map[string]struct {
		memType    string
		importance int
		keywords   []string
	}{
		"decision": {"decision", 2, []string{"decided", "decision", "agreed", "chose", "architecture"}},
		"fix":      {"episodic", 2, []string{"fixed", "bug", "resolved", "error was", "issue was"}},
		"learned":  {"semantic", 1, []string{"learned", "discovered", "found that", "realized"}},
	}

	for _, msg := range transcript {
		content, ok := msg["content"]
		if !ok || len(content) < 50 {
			continue
		}
		for _, sig := range signals {
			for _, kw := range sig.keywords {
				if strings.Contains(strings.ToLower(content), kw) {
					snippet := content
					if len(snippet) > 400 {
						snippet = snippet[:400] + "..."
					}
					out = append(out, snippetResult{
						content:    snippet,
						memType:    sig.memType,
						importance: sig.importance,
					})
					break
				}
			}
			if len(out) >= 3 {
				return out
			}
		}
	}
	return out
}

func buildActivitySummary(entries []activityEntry) string {
	if len(entries) == 0 {
		return ""
	}
	// Dedup by 50-char prefix
	seen := make(map[string]bool)
	var b strings.Builder
	b.WriteString("Session activity summary:\n")
	for _, e := range entries {
		prefix := e.Summary
		if len(prefix) > 50 {
			prefix = prefix[:50]
		}
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		b.WriteString(fmt.Sprintf("- [%s] %s\n", e.ToolName, e.Summary))
	}
	return b.String()
}
