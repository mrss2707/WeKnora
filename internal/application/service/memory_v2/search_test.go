package memory_v2

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestService() *MemoryServiceV2Impl {
	config := types.DefaultMemoryV2Config()
	return &MemoryServiceV2Impl{
		config:      config,
		tokenBudget: NewTokenBudgetManager(),
	}
}

func makeMemory(id, content string, importance int, verdict types.MemoryVerdict, sessionID string, createdAt time.Time) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		Content:    content,
		Importance: importance,
		Verdict:    verdict,
		SessionID:  sessionID,
		HubScore:   0,
		CreatedAt:  createdAt,
		MemoryType: "semantic",
		Tier:       2,
	}
}

func makeSearchResult(memory *types.AgentMemory, score float64) *types.MemorySearchResult {
	return &types.MemorySearchResult{
		Memory: memory,
		Score:  score,
	}
}

// ---------------------------------------------------------------------------
// Reciprocal Rank Fusion
// ---------------------------------------------------------------------------

func TestFuseRankings_BothSignalsRankFirst(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	a := makeMemory("a", "in both lists", 0, types.VerdictNone, "", now)
	b := makeMemory("b", "keyword only", 0, types.VerdictNone, "", now)
	c := makeMemory("c", "vector only", 0, types.VerdictNone, "", now)

	fused := svc.fuseRankings(
		[]*types.MemorySearchResult{makeSearchResult(a, 9), makeSearchResult(b, 3)},
		[]*types.MemorySearchResult{makeSearchResult(c, 0.9), makeSearchResult(a, 0.8)},
	)

	require.Len(t, fused, 3)
	scores := map[string]float64{}
	for _, r := range fused {
		scores[r.Memory.ID] = r.Score
	}
	assert.Greater(t, scores["a"], scores["c"], "found by both signals beats either alone")
	assert.Greater(t, scores["a"], scores["b"])
	assert.InDelta(t, (1.0/61+1.0/62)/(2.0/61), scores["a"], 1e-9, "normalized RRF")
	assert.LessOrEqual(t, scores["a"], 1.0)
}

func TestFuseRankings_ScaleFree(t *testing.T) {
	// Raw BM25 scores (unbounded) and cosine scores (0..1) never mix: only
	// ranks matter, so a huge BM25 score cannot dominate.
	svc := newTestService()
	now := time.Now()
	a := makeMemory("a", "a", 0, types.VerdictNone, "", now)
	b := makeMemory("b", "b", 0, types.VerdictNone, "", now)
	fused := svc.fuseRankings(
		[]*types.MemorySearchResult{makeSearchResult(a, 1000)},
		[]*types.MemorySearchResult{makeSearchResult(b, 0.99)},
	)
	require.Len(t, fused, 2)
	assert.InDelta(t, fused[0].Score, fused[1].Score, 1e-9)
}

func TestFuseRankings_DropsWeakVectorOnlyMatches(t *testing.T) {
	svc := newTestService()
	svc.config.MinCosine = 0.5
	now := time.Now()
	weak := makeMemory("weak", "unrelated", 0, types.VerdictNone, "", now)
	kw := makeMemory("kw", "keyword hit", 0, types.VerdictNone, "", now)
	strong := makeMemory("strong", "close", 0, types.VerdictNone, "", now)

	fused := svc.fuseRankings(
		[]*types.MemorySearchResult{makeSearchResult(kw, 1)},
		[]*types.MemorySearchResult{makeSearchResult(strong, 0.7), makeSearchResult(weak, 0.2), makeSearchResult(kw, 0.1)},
	)

	got := map[string]bool{}
	for _, r := range fused {
		got[r.Memory.ID] = true
	}
	assert.Equal(t, map[string]bool{"kw": true, "strong": true}, got, "keyword matches survive a low cosine; weak vector-only ones do not")
}

func TestFuseRankings_EmptyAndNil(t *testing.T) {
	svc := newTestService()
	assert.Empty(t, svc.fuseRankings(nil, nil))
	assert.Empty(t, svc.fuseRankings([]*types.MemorySearchResult{nil, {Memory: nil}}, nil))
}

func TestApplySignalBoost_ImportanceAndHub(t *testing.T) {
	svc := newTestService()
	now := time.Now()
	low := makeMemory("low", "x", -5, types.VerdictNone, "", now)
	high := makeMemory("high", "x", 6, types.VerdictNone, "", now)
	hub := makeMemory("hub", "x", 0, types.VerdictNone, "", now)
	hub.HubScore = 10
	rs := svc.applySignalBoost([]*types.MemorySearchResult{makeSearchResult(low, 1), makeSearchResult(high, 1), makeSearchResult(hub, 1)})
	assert.InDelta(t, 0.75, rs[0].Score, 1e-9)
	assert.InDelta(t, 1.3, rs[1].Score, 1e-9)
	assert.InDelta(t, 1.3, rs[2].Score, 1e-9, "hub capped at 3")
}

// ---------------------------------------------------------------------------
// Recency boost
// ---------------------------------------------------------------------------

func TestRecencyBoost_ShortTerm(t *testing.T) {
	svc := newTestService()
	svc.config.RecencyBoost.Enabled = true
	svc.config.RecencyBoost.ShortTermMultiplier = 1.15
	svc.config.RecencyBoost.ShortTermWindow = "1h"
	svc.config.RecencyBoost.LongTermFactor = 0.05
	svc.config.RecencyBoost.LongTermHalfLife = 30

	now := time.Now()
	mem := makeMemory("mem-1", "recent memory", 0, types.VerdictNone, "", now.Add(-30*time.Minute))
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	boosted := svc.applyRecencyBoost(results)

	// Age is 30 minutes, within 1h short-term window: score * 1.15
	require.InDelta(t, 0.5*1.15, boosted[0].Score, 0.0001, "short-term recency boost")
}

func TestRecencyBoost_LongTermDecay(t *testing.T) {
	svc := newTestService()
	svc.config.RecencyBoost.Enabled = true
	svc.config.RecencyBoost.ShortTermMultiplier = 1.15
	svc.config.RecencyBoost.ShortTermWindow = "1h"
	svc.config.RecencyBoost.LongTermFactor = 0.05
	svc.config.RecencyBoost.LongTermHalfLife = 30

	now := time.Now()
	// 90 days old -- well beyond the 1h short-term window
	mem := makeMemory("mem-1", "old memory", 0, types.VerdictNone, "", now.Add(-90*24*time.Hour))
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	boosted := svc.applyRecencyBoost(results)

	// Long-term: days = 90, factor = exp(-90/30 * 0.05) = exp(-0.15) ≈ 0.860708
	// score *= (1 + factor) / 2 = (1 + 0.860708) / 2 = 0.930354
	days := 90.0
	factor := math.Exp(-days / 30.0 * 0.05)
	expected := 1.0 * (1 + factor) / 2
	require.InDelta(t, expected, boosted[0].Score, 0.0001, "long-term recency boost")
}

func TestRecencyBoost_Disabled(t *testing.T) {
	svc := newTestService()
	svc.config.RecencyBoost.Enabled = false

	now := time.Now()
	mem := makeMemory("mem-1", "any memory", 0, types.VerdictNone, "", now.Add(-1*time.Minute))
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	boosted := svc.applyRecencyBoost(results)

	// Score unchanged when disabled
	require.InDelta(t, 0.5, boosted[0].Score, 0.0001, "no boost when disabled")
}

func TestRecencyBoost_DefaultFallback(t *testing.T) {
	// When config has zero values, defaults should kick in
	svc := newTestService()
	svc.config.RecencyBoost = types.RecencyBoostConfig{
		Enabled:             true,
		ShortTermMultiplier: 0,  // zero -> should fall through to default
		ShortTermWindow:     "", // empty -> should be set to "1h" then parsed to 0, defaulted to 1h
		LongTermFactor:      0,  // zero -> fine, still used
		LongTermHalfLife:    0,  // zero -> defaulted to 30
	}

	now := time.Now()
	mem := makeMemory("mem-1", "very recent", 0, types.VerdictNone, "", now.Add(-5*time.Minute))
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	boosted := svc.applyRecencyBoost(results)

	// Short-term window defaults to 1h after ParseDuration fails on ""
	// So 5 min is within short-term, score *= ShortTermMultiplier (0 -> 0)
	require.InDelta(t, 0.0, boosted[0].Score, 0.0001, "zero multiplier zeroes the score")
}

// RecencyBoost full pipeline: short-term and long-term should produce expected ordering
func TestRecencyBoost_NewerMemoryScoresHigher(t *testing.T) {
	svc := newTestService()
	svc.config.RecencyBoost.Enabled = true

	now := time.Now()
	memRecent := makeMemory("recent", "recent memory", 0, types.VerdictNone, "", now.Add(-10*time.Minute))
	memOld := makeMemory("old", "old memory", 0, types.VerdictNone, "", now.Add(-100*24*time.Hour))

	results := []*types.MemorySearchResult{
		makeSearchResult(memRecent, 0.5),
		makeSearchResult(memOld, 0.5),
	}

	boosted := svc.applyRecencyBoost(results)

	// After boost, recent should have higher score
	assert.Greater(t, boosted[0].Score, boosted[1].Score, "recent memory should score higher after recency boost")
}

// ---------------------------------------------------------------------------
// Verdict filtering and boost
// ---------------------------------------------------------------------------

func TestVerdictFilter_DefaultExcludesRefuted(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	memNone := makeMemory("mem-none", "normal", 0, types.VerdictNone, "", now)
	memRefuted := makeMemory("mem-refuted", "refuted", 0, types.VerdictRefuted, "", now)
	memDecision := makeMemory("mem-decision", "decision", 0, types.VerdictDecision, "", now)

	results := []*types.MemorySearchResult{
		makeSearchResult(memNone, 1.0),
		makeSearchResult(memRefuted, 1.0),
		makeSearchResult(memDecision, 1.0),
	}

	filtered := svc.applyVerdictFilter(results, &types.MemoryFilter{})

	// Default filter excludes refuted, boosts decision
	require.Len(t, filtered, 2, "refuted should be excluded by default")

	ids := make(map[string]bool)
	for _, r := range filtered {
		ids[r.Memory.ID] = true
	}
	assert.True(t, ids["mem-none"], "none verdict should be included")
	assert.True(t, ids["mem-decision"], "decision verdict should be included")
	assert.False(t, ids["mem-refuted"], "refuted verdict should be excluded")
}

func TestVerdictFilter_BoostsDecision(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-dec", "decision", 0, types.VerdictDecision, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	filtered := svc.applyVerdictFilter(results, &types.MemoryFilter{})
	require.Len(t, filtered, 1)
	assert.InDelta(t, 1.0*1.2, filtered[0].Score, 0.0001, "decision verdict should be boosted by 1.2x")
}

func TestVerdictFilter_BoostsFixed(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-fixed", "fixed", 0, types.VerdictFixed, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	filtered := svc.applyVerdictFilter(results, &types.MemoryFilter{})
	require.Len(t, filtered, 1)
	assert.InDelta(t, 1.0*1.1, filtered[0].Score, 0.0001, "fixed verdict should be boosted by 1.1x")
}

func TestVerdictFilter_ExplicitFilterIncludesRefuted(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	memRefuted := makeMemory("mem-ref", "refuted", 0, types.VerdictRefuted, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(memRefuted, 1.0),
	}

	// When verdicts are explicitly provided, refuted is not excluded
	filtered := svc.applyVerdictFilter(results, &types.MemoryFilter{
		Verdicts: []types.MemoryVerdict{types.VerdictRefuted},
	})
	require.Len(t, filtered, 1, "refuted should be included when explicitly requested")
}

func TestVerdictFilter_SkipsNilMemory(t *testing.T) {
	svc := newTestService()
	results := []*types.MemorySearchResult{
		{Memory: nil, Score: 1.0},
		{Memory: &types.AgentMemory{ID: "valid", Content: "ok", Verdict: types.VerdictNone}, Score: 1.0},
	}

	filtered := svc.applyVerdictFilter(results, &types.MemoryFilter{})
	require.Len(t, filtered, 1, "should skip nil memory entries")
	assert.Equal(t, "valid", filtered[0].Memory.ID)
}

// ---------------------------------------------------------------------------
// Session boost
// ---------------------------------------------------------------------------

func TestSessionBoost_SameSession(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-1", "in session", 0, types.VerdictNone, "session-42", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	boosted := svc.applySessionBoost(results, "session-42")
	assert.InDelta(t, 1.0*1.3, boosted[0].Score, 0.0001, "same session should be boosted by 1.3x")
}

func TestSessionBoost_DifferentSession(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-1", "different session", 0, types.VerdictNone, "session-99", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	boosted := svc.applySessionBoost(results, "session-42")
	assert.InDelta(t, 1.0, boosted[0].Score, 0.0001, "different session should not be boosted")
}

func TestSessionBoost_EmptySessionID(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-1", "no session filter", 0, types.VerdictNone, "session-42", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 1.0),
	}

	// Empty sessionID in filter means no boost at all
	boosted := svc.applySessionBoost(results, "")
	assert.InDelta(t, 1.0, boosted[0].Score, 0.0001, "empty session filter should not apply boost")
}

func TestSessionBoost_MultipleMemories(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	memIn := makeMemory("mem-in", "in session", 0, types.VerdictNone, "session-42", now)
	memOut := makeMemory("mem-out", "other session", 0, types.VerdictNone, "session-99", now)

	results := []*types.MemorySearchResult{
		makeSearchResult(memIn, 0.5),
		makeSearchResult(memOut, 0.5),
	}

	boosted := svc.applySessionBoost(results, "session-42")

	assert.InDelta(t, 0.5*1.3, boosted[0].Score, 0.0001, "in-session memory boosted")
	assert.InDelta(t, 0.5, boosted[1].Score, 0.0001, "other-session memory not boosted")
}

// ---------------------------------------------------------------------------
// Relative score floor
// ---------------------------------------------------------------------------

func TestScoreThreshold_RelativeToBest(t *testing.T) {
	svc := newTestService()
	svc.config.RelativeScoreFloor = 0.5
	now := time.Now()
	rs := []*types.MemorySearchResult{
		makeSearchResult(makeMemory("a", "a", 0, types.VerdictNone, "", now), 0.8),
		makeSearchResult(makeMemory("b", "b", 0, types.VerdictNone, "", now), 0.4),
		makeSearchResult(makeMemory("c", "c", 0, types.VerdictNone, "", now), 0.39),
	}
	got := svc.applyScoreThreshold(rs)
	require.Len(t, got, 2)
	assert.Equal(t, "b", got[1].Memory.ID, "exactly half of the best is kept")
}

func TestScoreThreshold_DisabledAndEmpty(t *testing.T) {
	svc := newTestService()
	svc.config.RelativeScoreFloor = 0
	now := time.Now()
	rs := []*types.MemorySearchResult{makeSearchResult(makeMemory("a", "a", 0, types.VerdictNone, "", now), 0.01)}
	assert.Len(t, svc.applyScoreThreshold(rs), 1)
	assert.Empty(t, svc.applyScoreThreshold(nil))
}

// ---------------------------------------------------------------------------
// Token budget modes
// ---------------------------------------------------------------------------

func TestTokenBudget_FullMode(t *testing.T) {
	tb := NewTokenBudgetManager()
	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 1500,
		SummaryThreshold:  2500,
		MaxPerMemory:      300,
	}

	// Content under 1500 tokens (~6000 chars)
	mem := makeMemory("mem-1", strings.Repeat("hello world ", 100), 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "full", info.Mode)
	require.Len(t, adjusted, 1)
	assert.Equal(t, "full", info.Mode, "should be full mode when under truncate threshold")
	assert.GreaterOrEqual(t, info.Remaining, 0, "remaining should be >= 0")
}

func TestTokenBudget_TruncatedMode(t *testing.T) {
	tb := NewTokenBudgetManager()
	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 100,  // below content's ~137 token estimate
		SummaryThreshold:  1000, // above content's ~137 token estimate
		MaxPerMemory:      100,  // 100 chars max per memory
	}

	// Content ~550 chars, ~137 tokens
	content := strings.Repeat("This is a test sentence for truncation mode. ", 10) // ~550 chars
	mem := makeMemory("mem-1", content, 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "truncated", info.Mode)
	require.Len(t, adjusted, 1)
	assert.LessOrEqual(t, len(adjusted[0].Memory.Content), 100, "content should be truncated to MaxPerMemory")
}

func TestTokenBudget_SummaryMode(t *testing.T) {
	tb := NewTokenBudgetManager()
	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 500,
		SummaryThreshold:  600, // trigger summary mode even with moderate content
		MaxPerMemory:      100,
	}

	// Content ~550 chars, ~137 tokens — over summary threshold 600 chars (~150 tokens) but wait...
	// Actually, threshold is checked via estimated tokens: totalChars / 4
	// We need estimateTokens > 600/4 = 150 tokens... no, SummaryThreshold is a token count.
	// Let me re-read the logic:

	// totalTokens := tb.estimateTokens(results)  // totalChars / 4
	// switch {
	// case totalTokens <= budget.TruncateThreshold: -> full
	// case totalTokens <= budget.SummaryThreshold: -> truncated
	// default: -> summary
	// }

	// If TruncateThreshold=500, SummaryThreshold=600, we need totalTokens > 600 for summary.
	// That's > 2400 chars. Let's use more content.

	content := strings.Repeat("This is test content for summary mode. ", 100) // ~3500 chars, ~875 tokens
	mem := makeMemory("mem-1", content, 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "summary", info.Mode)
	// Since chat is nil, summary falls back to returning original results
	require.Len(t, adjusted, 1)
}

func TestTokenBudget_HardCapForcesSummary(t *testing.T) {
	tb := NewTokenBudgetManager()
	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    100, // very low hard cap
		TruncateThreshold: 200,
		SummaryThreshold:  300,
		MaxPerMemory:      100,
	}

	content := strings.Repeat("hello world ", 50) // ~550 chars, ~137 tokens > MaxTotalTokens=100
	mem := makeMemory("mem-1", content, 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "summary", info.Mode, "hard cap should force summary mode")
	_ = adjusted
}

func TestTokenBudget_EmptyResults(t *testing.T) {
	tb := NewTokenBudgetManager()
	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 1500,
		SummaryThreshold:  2500,
		MaxPerMemory:      300,
	}

	adjusted, info := tb.Apply(context.Background(), nil, budget)
	assert.Equal(t, "full", info.Mode)
	assert.Equal(t, 0, info.Used)
	assert.Equal(t, 2000, info.Remaining)
	assert.Empty(t, adjusted)
}

func TestTokenBudget_EstimateTokens(t *testing.T) {
	tb := NewTokenBudgetManager()

	// ~4 chars per token
	mem := makeMemory("mem-1", "hello world how are you doing today", 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	estimated := tb.estimateTokens(results)
	expected := len("hello world how are you doing today") / 4
	assert.Equal(t, expected, estimated)
}

func TestTokenBudget_Truncate(t *testing.T) {
	tb := NewTokenBudgetManager()

	content := "hello world this is a test"
	truncated := tb.truncate(content, 10)
	assert.Equal(t, "hello worl", truncated, "should truncate to first 10 characters")

	// Shorter than max, should return full
	full := tb.truncate("short", 10)
	assert.Equal(t, "short", full, "content shorter than max should not be truncated")
}

func TestTokenBudget_SummarizeWithoutChat(t *testing.T) {
	tb := NewTokenBudgetManager()
	// No chat set — should fallback

	now := time.Now()
	mem := makeMemory("mem-1", "test memory content", 0, types.VerdictNone, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, mode := tb.summarize(context.Background(), results)
	assert.Equal(t, "summary", mode, "fallback mode should be 'summary'")
	assert.Len(t, adjusted, 1, "should return original results in fallback")
}

func TestTokenBudget_SummarizeEmptyResults(t *testing.T) {
	tb := NewTokenBudgetManager()
	adjusted, mode := tb.summarize(context.Background(), nil)
	assert.Equal(t, "summary", mode)
	assert.Empty(t, adjusted)
}

// ---------------------------------------------------------------------------
// Context packing (XML output)
// ---------------------------------------------------------------------------

func TestPackContext_ValidXML(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-1", "test content", 3, types.VerdictDecision, "session-42", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.85),
	}

	xml := svc.packContext(context.Background(), "test query", results)

	// Should start with <memory_context> and end with </memory_context>
	assert.True(t, strings.HasPrefix(xml, "<memory_context>"), "should start with memory_context tag")
	assert.True(t, strings.HasSuffix(xml, "</memory_context>"), "should end with memory_context tag")

	// Should contain basic elements
	assert.Contains(t, xml, `<metadata query="test query"`)
	assert.Contains(t, xml, `<token_budget mode=`)
	assert.Contains(t, xml, `<result_count>1</result_count>`)
	assert.Contains(t, xml, `<memory id="mem-1"`)
	assert.Contains(t, xml, `<type>semantic</type>`)
	assert.Contains(t, xml, `<verdict>decision</verdict>`)
	assert.Contains(t, xml, `<importance>3</importance>`)
	assert.Contains(t, xml, `<score>0.8500</score>`)
	assert.Contains(t, xml, `<content>test content</content>`)
	assert.Contains(t, xml, `<session_id>session-42</session_id>`)
}

func TestPackContext_NoSessionID(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	mem := makeMemory("mem-1", "no session", 0, types.VerdictNone, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	xml := svc.packContext(context.Background(), "", results)

	assert.NotContains(t, xml, "<session_id>", "should not include session_id when empty")
}

func TestPackContext_StaleFlag(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	// Simulate a stale memory: old + low importance
	mem := makeMemory("mem-1", "old memory", 0, types.VerdictNone, "", now.Add(-365*24*time.Hour))
	result := makeSearchResult(mem, 0.3)
	result.IsStale = true
	result.StaleDays = 365

	results := []*types.MemorySearchResult{result}

	xml := svc.packContext(context.Background(), "query", results)

	assert.Contains(t, xml, `<stale days="365" />`, "should include stale tag for stale memories")
}

func TestPackContext_EmptyResults(t *testing.T) {
	svc := newTestService()

	xml := svc.packContext(context.Background(), "empty", nil)

	assert.Contains(t, xml, "<memory_context>")
	assert.Contains(t, xml, `<result_count>0</result_count>`)
	assert.NotContains(t, xml, "<memory id=")
}

func TestPackContext_EscapesXMLContent(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	// Content with XML special chars
	mem := makeMemory("mem-1", "content with <tag> & \"quotes\" and 'apos'", 0, types.VerdictNone, "", now)
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	xml := svc.packContext(context.Background(), "q", results)

	assert.Contains(t, xml, "&lt;tag&gt;")
	assert.Contains(t, xml, "&amp;")
	assert.Contains(t, xml, "&quot;quotes&quot;")
	assert.Contains(t, xml, "&apos;apos&apos;")
	assert.NotContains(t, xml, "<tag>")
}

func TestPackContext_MultipleMemories(t *testing.T) {
	svc := newTestService()
	now := time.Now()

	memA := makeMemory("mem-a", "first", 0, types.VerdictNone, "", now)
	memB := makeMemory("mem-b", "second", 0, types.VerdictNone, "", now)

	results := []*types.MemorySearchResult{
		makeSearchResult(memA, 0.9),
		makeSearchResult(memB, 0.8),
	}

	xml := svc.packContext(context.Background(), "multi", results)

	assert.Contains(t, xml, `index="1"`)
	assert.Contains(t, xml, `index="2"`)
	assert.Contains(t, xml, `<result_count>2</result_count>`)
}

// ---------------------------------------------------------------------------
// escapeXML
// ---------------------------------------------------------------------------

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"ampersand", "a & b", "a &amp; b"},
		{"less than", "<tag>", "&lt;tag&gt;"},
		{"greater than", "a > b", "a &gt; b"},
		{"single quote", "it's", "it&apos;s"},
		{"double quote", `say "hello"`, "say &quot;hello&quot;"},
		{"all special chars", `<a & b>'c' "d"`, "&lt;a &amp; b&gt;&apos;c&apos; &quot;d&quot;"},
		{"plain text", "hello world", "hello world"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeXML(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEscapeXML_Ordering(t *testing.T) {
	// & must be replaced first to avoid double-escaping
	input := "&lt;"
	output := escapeXML(input)
	assert.Equal(t, "&amp;lt;", output, "& should be escaped first, before lt")
}

// ---------------------------------------------------------------------------
// Keyword extraction
// ---------------------------------------------------------------------------

func TestExtractKeywordsFromQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "removes stop words",
			query:    "the quick brown fox jumps",
			expected: "quick brown fox jumps",
		},
		{
			name:     "removes short words",
			query:    "a of in it is my cat",
			expected: "cat",
		},
		{
			name:     "strips punctuation",
			query:    "hello, world! test?",
			expected: "hello world test",
		},
		{
			name:     "all stop words returns original",
			query:    "the and of in",
			expected: "the and of in",
		},
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
		{
			name:     "only punctuation",
			query:    "?!,.",
			expected: "?!,.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeywordsFromQuery(tt.query)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// SearchMemories pipeline integration (with mock repo and embedder)
// ---------------------------------------------------------------------------

// mockRepo implements the minimum of interfaces.MemoryRepositoryV2 for testing SearchMemories
type mockSearchRepo struct {
	searchFunc       func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error)
	cosineSearchFunc func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)
}

func (m *mockSearchRepo) Create(ctx context.Context, memory *types.AgentMemory) error { return nil }
func (m *mockSearchRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockSearchRepo) Update(ctx context.Context, memory *types.AgentMemory) error { return nil }
func (m *mockSearchRepo) Delete(ctx context.Context, tenantID, id string) error       { return nil }
func (m *mockSearchRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}
func (m *mockSearchRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}
func (m *mockSearchRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	return true, nil
}
func (m *mockSearchRepo) UnlockDreamer(ctx context.Context, tenantID string) error    { return nil }
func (m *mockSearchRepo) ComputeHubScores(ctx context.Context, tenantID string) error { return nil }
func (m *mockSearchRepo) InvalidateResultCache(ctx context.Context, tenantID string)  {}
func (m *mockSearchRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockSearchRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockSearchRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockSearchRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockSearchRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockSearchRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}
func (m *mockSearchRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}

// mockEmbedder implements embedding.Embedder
type mockEmbedder struct {
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}
func (m *mockEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}
func (m *mockEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return nil, nil
}
func (m *mockEmbedder) GetModelName() string { return "mock" }
func (m *mockEmbedder) GetDimensions() int   { return 3 }
func (m *mockEmbedder) GetModelID() string   { return "mock-model" }

func TestSearchMemories_FullPipeline(t *testing.T) {
	now := time.Now()
	mem := makeMemory("mem-1", "test memory", 0, types.VerdictNone, "", now)

	svc := &MemoryServiceV2Impl{
		repo: &mockSearchRepo{
			searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
				return []*types.MemorySearchResult{makeSearchResult(mem, 0.0)}, 1, nil
			},
			cosineSearchFunc: func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
				return []*types.MemorySearchResult{makeSearchResult(mem, 0.9)}, nil
			},
		},
		embedder: &mockEmbedder{
			embedFunc: func(ctx context.Context, text string) ([]float32, error) {
				return []float32{0.1, 0.2, 0.3}, nil
			},
		},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}

	results, err := svc.SearchMemories(context.Background(), "test", &types.MemoryFilter{
		TenantID: "t1",
		Limit:    10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, "mem-1", results[0].Memory.ID)
	assert.Greater(t, results[0].Score, 0.0, "score should be positive after merging")
}

func TestSearchMemories_EmptyQuery(t *testing.T) {
	// Empty query should still work: BM25 only, no embedding
	now := time.Now()
	mem := makeMemory("mem-1", "test", 0, types.VerdictNone, "", now)

	svc := &MemoryServiceV2Impl{
		repo: &mockSearchRepo{
			searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
				return []*types.MemorySearchResult{makeSearchResult(mem, 0.0)}, 1, nil
			},
		},
		embedder:    &mockEmbedder{},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}
	results, err := svc.SearchMemories(context.Background(), "", &types.MemoryFilter{
		TenantID: "t1",
		Limit:    10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
}

func TestSearchMemories_BothSearchesFail(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		repo: &mockSearchRepo{
			searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
				return nil, 0, assert.AnError
			},
			cosineSearchFunc: func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
				return nil, assert.AnError
			},
		},
		embedder:    &mockEmbedder{},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}

	results, err := svc.SearchMemories(context.Background(), "test", &types.MemoryFilter{
		TenantID: "t1",
		Limit:    10,
	})
	require.NoError(t, err, "should not error even when both searches fail")
	assert.Empty(t, results, "no results when both searches fail")
}

func TestSearchMemories_EmbeddingReturnsEmpty(t *testing.T) {
	now := time.Now()
	mem := makeMemory("mem-1", "test", 0, types.VerdictNone, "", now)

	svc := &MemoryServiceV2Impl{
		repo: &mockSearchRepo{
			searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
				return []*types.MemorySearchResult{makeSearchResult(mem, 0.0)}, 1, nil
			},
		},
		embedder: &mockEmbedder{
			embedFunc: func(ctx context.Context, text string) ([]float32, error) {
				return nil, nil
			},
		},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}
	// BM25-only scores are below default threshold 0.4
	svc.config.MinScoreThreshold = 0.1

	results, err := svc.SearchMemories(context.Background(), "test", &types.MemoryFilter{
		TenantID: "t1",
		Limit:    10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results, "BM25 results should still work when embedding fails")
}

func TestSearchMemories_NilFilter(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		config: types.DefaultMemoryV2Config(),
	}
	_, err := svc.SearchMemories(context.Background(), "test", nil)
	require.Error(t, err, "nil filter should return error")
	assert.Contains(t, err.Error(), "filter is required")
}

// ---------------------------------------------------------------------------
// Chat interface for summary mode
// ---------------------------------------------------------------------------

// mockChat implements chat.Chat for testing summary mode
type mockChat struct {
	chatFunc func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error)
}

func (m *mockChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, opts)
	}
	return &types.ChatResponse{Content: "summarized memory content"}, nil
}
func (m *mockChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}
func (m *mockChat) GetModelName() string { return "mock-chat" }
func (m *mockChat) GetModelID() string   { return "mock-chat-model" }

func TestTokenBudget_SummarizeWithChat(t *testing.T) {
	tb := NewTokenBudgetManager().WithChat(&mockChat{})

	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 500,
		SummaryThreshold:  600, // trigger summary
		MaxPerMemory:      100,
	}

	content := strings.Repeat("This is test content for summary mode. ", 100)
	mem := makeMemory("mem-1", content, 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "summary", info.Mode)
	require.Len(t, adjusted, 1)
	// Chat mock returns "summarized memory content"
	assert.Equal(t, "summarized memory content", adjusted[0].Memory.Content)
}

func TestTokenBudget_SummarizeChatError(t *testing.T) {
	tb := NewTokenBudgetManager().WithChat(&mockChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return nil, assert.AnError
		},
	})

	budget := types.TokenBudgetConfig{
		MaxTotalTokens:    2000,
		TruncateThreshold: 500,
		SummaryThreshold:  600,
		MaxPerMemory:      100,
	}

	content := strings.Repeat("This is test content. ", 200) // ~4400 chars, ~1100 tokens
	mem := makeMemory("mem-1", content, 0, types.VerdictNone, "", time.Now())
	results := []*types.MemorySearchResult{
		makeSearchResult(mem, 0.5),
	}

	adjusted, info := tb.Apply(context.Background(), results, budget)
	assert.Equal(t, "summary", info.Mode)
	require.Len(t, adjusted, 1, "should fallback to original results when chat errors")
}

// ---------------------------------------------------------------------------
// RetrieveMemory (bridge)
// ---------------------------------------------------------------------------

func TestMemoryContextBridge(t *testing.T) {
	t.Run("empty query", func(t *testing.T) {
		svc := newTestService()
		ctx := context.Background()

		memCtx, err := svc.RetrieveMemory(ctx, "user-1", "")
		require.NoError(t, err)
		assert.Empty(t, memCtx.RelatedEpisodes, "empty query should return empty context")
	})

	t.Run("with results", func(t *testing.T) {
		now := time.Now()
		mem := makeMemory("mem-bridge-1", "user mentioned they prefer clean architecture with dependency injection", 4, types.VerdictDecision, "session-1", now)

		svc := &MemoryServiceV2Impl{
			repo: &mockSearchRepo{
				searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
					return []*types.MemorySearchResult{makeSearchResult(mem, 0.0)}, 1, nil
				},
				cosineSearchFunc: func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
					return []*types.MemorySearchResult{makeSearchResult(mem, 0.85)}, nil
				},
			},
			embedder: &mockEmbedder{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return []float32{0.1, 0.2, 0.3}, nil
				},
			},
			config:      types.DefaultMemoryV2Config(),
			tokenBudget: NewTokenBudgetManager(),
		}
		svc.config.MinScoreThreshold = 0.1 // low enough to keep merged scores

		ctx := context.Background()
		memCtx, err := svc.RetrieveMemory(ctx, "user-42", "clean architecture preferences")
		require.NoError(t, err)

		// Bridge format: MemoryContext with exactly one RelatedEpisode
		require.Len(t, memCtx.RelatedEpisodes, 1, "should have exactly one related episode")
		assert.Empty(t, memCtx.RelatedEntities, "related entities should be empty")
		assert.Empty(t, memCtx.RelatedRelations, "related relations should be empty")

		ep := memCtx.RelatedEpisodes[0]
		assert.Equal(t, "user-42", ep.UserID, "episode UserID should match the userID passed to RetrieveMemory")
		assert.Empty(t, ep.SessionID, "episode SessionID should be empty (not set by bridge)")

		// Summary should contain valid XML from packContext
		summary := ep.Summary
		assert.True(t, strings.HasPrefix(summary, "<memory_context>"), "summary should start with memory_context tag")
		assert.True(t, strings.HasSuffix(summary, "</memory_context>"), "summary should end with memory_context tag")
		assert.Contains(t, summary, `<memory id="mem-bridge-1"`)
		assert.Contains(t, summary, `<content>user mentioned they prefer clean architecture with dependency injection</content>`)
		assert.Contains(t, summary, `<type>semantic</type>`)
		assert.Contains(t, summary, `<verdict>decision</verdict>`)
		assert.Contains(t, summary, `<session_id>session-1</session_id>`)
	})

	t.Run("empty results", func(t *testing.T) {
		svc := &MemoryServiceV2Impl{
			repo: &mockSearchRepo{
				searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
					return nil, 0, nil
				},
			},
			embedder: &mockEmbedder{},
			config:   types.DefaultMemoryV2Config(),
		}

		ctx := context.Background()
		memCtx, err := svc.RetrieveMemory(ctx, "user-1", "something not in memory")
		require.NoError(t, err, "empty results should not cause an error")
		assert.Empty(t, memCtx.RelatedEpisodes, "empty results should return empty MemoryContext")
		assert.Empty(t, memCtx.RelatedEntities, "empty results should have no entities")
		assert.Empty(t, memCtx.RelatedRelations, "empty results should have no relations")
	})

	t.Run("search error", func(t *testing.T) {
		// SearchMemories swallows BM25 and cosine errors internally (logs them and
		// falls back to empty/nil results), so the error path in RetrieveMemory is
		// unreachable via repo-level errors. Instead, SearchMemories returns empty
		// results, and RetrieveMemory returns an empty MemoryContext with no error.
		svc := &MemoryServiceV2Impl{
			repo: &mockSearchRepo{
				searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
					return nil, 0, assert.AnError
				},
			},
			embedder: &mockEmbedder{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return nil, assert.AnError
				},
			},
			config:      types.DefaultMemoryV2Config(),
			tokenBudget: NewTokenBudgetManager(),
		}

		ctx := context.Background()
		memCtx, err := svc.RetrieveMemory(ctx, "user-1", "something that errors")
		require.NoError(t, err, "search error should be swallowed by SearchMemories")
		assert.Empty(t, memCtx.RelatedEpisodes, "error case should return empty MemoryContext")
		assert.Empty(t, memCtx.RelatedEntities, "error case should have no entities")
		assert.Empty(t, memCtx.RelatedRelations, "error case should have no relations")
	})
}

// ---------------------------------------------------------------------------
// Staleness computation
// ---------------------------------------------------------------------------

func TestSearchMemories_StalenessComputed(t *testing.T) {
	now := time.Now()
	oldMem := makeMemory("mem-old", "old memory", 0, types.VerdictNone, "", now.Add(-200*24*time.Hour))
	recentMem := makeMemory("mem-recent", "recent memory", 2, types.VerdictNone, "", now.Add(-30*24*time.Hour))

	svc := &MemoryServiceV2Impl{
		repo: &mockSearchRepo{
			searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
				return []*types.MemorySearchResult{
					makeSearchResult(oldMem, 0.0),
					makeSearchResult(recentMem, 0.0),
				}, 2, nil
			},
		},
		embedder:    &mockEmbedder{},
		config:      types.DefaultMemoryV2Config(),
		tokenBudget: NewTokenBudgetManager(),
	}
	// BM25-only scores are below default threshold 0.4
	svc.config.MinScoreThreshold = 0.05

	results, err := svc.SearchMemories(context.Background(), "test", &types.MemoryFilter{
		TenantID: "t1",
		Limit:    10,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	for _, r := range results {
		if r.Memory.ID == "mem-old" {
			assert.True(t, r.IsStale, "old + low importance should be stale")
			assert.Greater(t, r.StaleDays, 180)
		}
		if r.Memory.ID == "mem-recent" {
			assert.False(t, r.IsStale, "recent + high importance should not be stale")
		}
	}

}

// TestIsDimensionMismatch verifies the pgvector dimension mismatch detection.
func TestIsDimensionMismatch(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected bool
	}{
		{"nil error", nil, false},
		{"exact PG message", fmt.Errorf("ERROR: different vector dimensions 768 and 2000 (SQLSTATE 22000)"), true},
		{"substring match", fmt.Errorf("cosine search failed: ERROR: different vector dimensions 768 and 2000"), true},
		{"unrelated error", fmt.Errorf("connection refused"), false},
		{"empty error", fmt.Errorf(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDimensionMismatch(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// SaveMemory nil guard
// ---------------------------------------------------------------------------

func TestSaveMemory_NilMemory(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		config: types.DefaultMemoryV2Config(),
	}

	result, err := svc.SaveMemory(context.Background(), nil)
	assert.Error(t, err, "should return error for nil memory")
	assert.Nil(t, result, "should not return result for nil memory")
	assert.Contains(t, err.Error(), "nil memory", "error should mention nil memory")
}

func TestSaveMemory_ValidateContentRejectsOverMax(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		config: types.DefaultMemoryV2Config(),
	}

	longContent := strings.Repeat("x", 10001)
	memory := &types.AgentMemory{
		TenantID: "t1",
		Content:  longContent,
	}

	result, err := svc.SaveMemory(context.Background(), memory)
	assert.Error(t, err, "should return error for content > 10000 chars")
	assert.Nil(t, result, "should not return result")
	var validationErr *ErrMemoryValidation
	assert.ErrorAs(t, err, &validationErr, "error should be ErrMemoryValidation")
	assert.Contains(t, err.Error(), "exceeds maximum", "error should mention exceeding max")
}

func TestSaveMemory_ValidateContentRejectsUnderMin(t *testing.T) {
	svc := &MemoryServiceV2Impl{
		config: types.DefaultMemoryV2Config(),
	}

	memory := &types.AgentMemory{
		TenantID: "t1",
		Content:  "short",
	}

	result, err := svc.SaveMemory(context.Background(), memory)
	assert.Error(t, err, "should return error for content < 10 chars")
	assert.Nil(t, result)
	var validationErr *ErrMemoryValidation
	assert.ErrorAs(t, err, &validationErr, "error should be ErrMemoryValidation")
}
