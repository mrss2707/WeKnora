package memory_v2

import (
	"context"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// TokenBudgetManager handles token estimation, truncation, and summarization
// of memory search results to fit within the configured budget.
type TokenBudgetManager struct {
	chat chat.Chat
}

// NewTokenBudgetManager creates a new TokenBudgetManager.
func NewTokenBudgetManager() *TokenBudgetManager {
	return &TokenBudgetManager{}
}

// WithChat sets the chat model for summary mode (lazy init).
func (tb *TokenBudgetManager) WithChat(ch chat.Chat) *TokenBudgetManager {
	tb.chat = ch
	return tb
}

// estimateTokens heuristically estimates the number of tokens from characters.
// Go benchmark: ~4 chars per token for English text.
func (tb *TokenBudgetManager) estimateTokens(results []*types.MemorySearchResult) int {
	totalChars := 0
	for _, r := range results {
		if r.Memory != nil {
			totalChars += len(r.Memory.Content)
		}
	}
	if totalChars == 0 {
		return 0
	}
	return totalChars / 4
}

// truncate shortens a string to at most maxChars runes.
func (tb *TokenBudgetManager) truncate(content string, maxChars int) string {
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	return string(runes[:maxChars])
}

// summarize uses a language model to compress search results into a brief summary.
func (tb *TokenBudgetManager) summarize(ctx context.Context, results []*types.MemorySearchResult) ([]*types.MemorySearchResult, string) {
	if tb.chat == nil || len(results) == 0 {
		// Fallback: just return truncated results
		return results, "summary"
	}

	// Build a compact text from results
	var compact string
	for _, r := range results {
		if r.Memory != nil {
			compact += "- " + r.Memory.Content + "\n"
		}
	}

	// Truncate input to avoid exceeding context
	if len(compact) > 8000 {
		compact = compact[:8000]
	}

	prompt := "Summarize the following memories into a single concise paragraph (max 200 words). " +
		"Include the key facts, decisions, and preferences. " +
		"Exclude redundant or outdated information.\n\nMemories:\n" + compact

	resp, err := tb.chat.Chat(ctx, []chat.Message{
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		// Fallback: truncate to fit
		return results, "summary"
	}

	summary := resp.Content
	// Create a single summary result
	summaryResult := &types.MemorySearchResult{
		Memory: &types.AgentMemory{
			Content:     summary,
			MemoryType:  "semantic",
			Importance:  0,
			Tier:        2,
			Verdict:     types.VerdictNone,
		},
	}

	return []*types.MemorySearchResult{summaryResult}, "summary"
}

// Apply applies the token budget algorithm to search results.
func (tb *TokenBudgetManager) Apply(ctx context.Context, results []*types.MemorySearchResult, budget types.TokenBudgetConfig) ([]*types.MemorySearchResult, types.TokenBudgetInfo) {
	totalTokens := tb.estimateTokens(results)

	// Hard cap: if over budget, force summary mode
	if totalTokens > budget.MaxTotalTokens {
		summarized, _ := tb.summarize(ctx, results)
		used := tb.estimateTokens(summarized)
		return summarized, types.TokenBudgetInfo{
			Mode:      "summary",
			Used:      used,
			Remaining: budget.MaxTotalTokens - used,
		}
	}

	switch {
	case totalTokens <= budget.TruncateThreshold:
		return results, types.TokenBudgetInfo{
			Mode:      "full",
			Used:      totalTokens,
			Remaining: budget.MaxTotalTokens - totalTokens,
		}

	case totalTokens <= budget.SummaryThreshold:
		for _, r := range results {
			if r.Memory != nil {
				r.Memory.Content = tb.truncate(r.Memory.Content, budget.MaxPerMemory)
			}
		}
		used := tb.estimateTokens(results)
		return results, types.TokenBudgetInfo{
			Mode:      "truncated",
			Used:      used,
			Remaining: budget.MaxTotalTokens - used,
		}

	default:
		summarized, _ := tb.summarize(ctx, results)
		used := tb.estimateTokens(summarized)
		return summarized, types.TokenBudgetInfo{
			Mode:      "summary",
			Used:      used,
			Remaining: budget.MaxTotalTokens - used,
		}
	}
}
