package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DreamerWorker periodically runs LLM-driven consolidation (the "dreamer").
type DreamerWorker struct {
	repo       interfaces.MemoryRepositoryV2
	chat       chat.Chat
	config     types.DreamerConfig
	interval   time.Duration
	workerID   string
	tenantIDs  []string
}

// NewDreamerWorker creates a new DreamerWorker.
func NewDreamerWorker(repo interfaces.MemoryRepositoryV2, chat chat.Chat, config types.DreamerConfig, tenantIDs []string) *DreamerWorker {
	return &DreamerWorker{
		repo:      repo,
		chat:      chat,
		config:    config,
		interval:  parseDuration(config.Interval, 1*time.Hour),
		workerID:  fmt.Sprintf("dreamer-%d", time.Now().UnixNano()),
		tenantIDs: tenantIDs,
	}
}

// Run starts the dreamer worker loop.
func (d *DreamerWorker) Run(ctx context.Context) {
	if !d.config.Enabled {
		logger.Infof(ctx, "dreamer: disabled, not starting")
		return
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.dreamPass(ctx)
		}
	}
}

// RunPass executes a single dreamer pass for a given tenant.
// This is the public entry point called from the HTTP handler (TriggerDream).
func (d *DreamerWorker) RunPass(ctx context.Context, tenantID string) (*types.DreamResult, error) {
	if !d.config.Enabled {
		return &types.DreamResult{}, nil
	}

	return d.executePass(ctx, tenantID)
}

// dreamPass runs the dreamer for each known tenant.
func (d *DreamerWorker) dreamPass(ctx context.Context) {
	if len(d.tenantIDs) == 0 {
		// Fallback to empty tenant for backward compatibility
		result, err := d.executePass(ctx, "")
		if err != nil {
			logger.Errorf(ctx, "dreamer: pass failed: %v", err)
			return
		}
		logger.Infof(ctx, "dreamer: pass complete: %d proposed, %d applied, %d tokens used",
			result.ActionsProposed, result.ActionsApplied, result.TokenUsed)
		return
	}

	for _, tid := range d.tenantIDs {
		result, err := d.executePass(ctx, tid)
		if err != nil {
			logger.Errorf(ctx, "dreamer: pass failed for tenant %s: %v", tid, err)
			continue
		}
		logger.Infof(ctx, "dreamer: pass complete for tenant %s: %d proposed, %d applied, %d tokens used",
			tid, result.ActionsProposed, result.ActionsApplied, result.TokenUsed)
	}
}

// executePass runs the full dreamer pipeline for a tenant.
func (d *DreamerWorker) executePass(ctx context.Context, tenantID string) (*types.DreamResult, error) {
	result := &types.DreamResult{
		Actions: []types.DreamAction{},
	}

	// 1. Acquire dreamer lock
	acquired, err := d.repo.TryDreamerLock(ctx, tenantID, d.workerID)
	if err != nil {
		return result, fmt.Errorf("dreamer lock acquire failed: %w", err)
	}
	if !acquired {
		logger.Infof(ctx, "dreamer: lock not acquired for tenant %s (another worker holds it)", tenantID)
		return result, nil
	}

	// Ensure unlock on return
	defer func() {
		if err := d.repo.UnlockDreamer(ctx, tenantID); err != nil {
			logger.Errorf(ctx, "dreamer: unlock failed: %v", err)
		}
	}()

	// 2. Fetch candidate memories
	filter := &types.MemoryFilter{
		TenantID: tenantID,
		Limit:    50,
	}
	candidates, _, err := d.repo.Search(ctx, filter)
	if err != nil {
		return result, fmt.Errorf("dreamer: search failed: %w", err)
	}

	if len(candidates) == 0 {
		logger.Infof(ctx, "dreamer: no candidate memories for tenant %s", tenantID)
		return result, nil
	}

	// 3. Build prompt
	prompt := d.buildDreamerPrompt(candidates)

	// 4. Set token budget
	maxTokens := d.config.TokenBudget
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	maxActions := d.config.MaxActions
	if maxActions <= 0 {
		maxActions = 5
	}

	opts := &chat.ChatOptions{
		MaxTokens: maxTokens,
	}

	// 5. Call LLM
	resp, err := d.chat.Chat(ctx, []chat.Message{
		{Role: "system", Content: "You are a memory consolidation system. Analyze memories and propose actions to improve the knowledge base. Output ONLY valid JSON."},
		{Role: "user", Content: prompt},
	}, opts)
	if err != nil {
		return result, fmt.Errorf("dreamer: LLM call failed: %w", err)
	}

	result.TokenUsed = maxTokens // approximate

	// 6. Parse response
	var dreamResp dreamerResponse
	if err := json.Unmarshal([]byte(resp.Content), &dreamResp); err != nil {
		logger.Errorf(ctx, "dreamer: failed to parse LLM response: %v", err)
		return result, nil
	}

	// 7. Validate and apply actions
	for _, action := range dreamResp.Actions {
		if len(result.Actions) >= maxActions {
			break
		}
		result.ActionsProposed++

		if !d.validateAction(action) {
			continue
		}

		if d.config.DryRun {
			result.Actions = append(result.Actions, action)
			result.ActionsApplied++
			continue
		}

		if err := d.applyAction(ctx, tenantID, action); err != nil {
			logger.Errorf(ctx, "dreamer: action apply failed: %v", err)
			continue
		}
		result.Actions = append(result.Actions, action)
		result.ActionsApplied++
	}

	return result, nil
}

// buildDreamerPrompt constructs the prompt for the dreamer LLM call.
func (d *DreamerWorker) buildDreamerPrompt(candidates []*types.MemorySearchResult) string {
	var b strings.Builder
	b.WriteString("Review the following memories and propose up to ")
	b.WriteString(fmt.Sprintf("%d", d.config.MaxActions))
	b.WriteString(" consolidation actions.\n\n")
	b.WriteString("Available action types:\n")
	b.WriteString("- update_verdict: Change the verdict of a memory (target_id, new_verdict). ")
	b.WriteString("Valid verdicts: none, fixed, refuted, decision, gotcha, wip. ")
	b.WriteString("DO NOT change protected verdicts (decision, fixed).\n")
	b.WriteString("- adjust_importance: Change importance score (target_id, delta from -3 to +3)\n")
	b.WriteString("- merge: Combine two similar memories (target_ids array)\n")
	b.WriteString("- remove: Soft-delete low-quality or irrelevant memory (target_id, tier must be >= 2)\n\n")
	b.WriteString("For each action, provide confidence (0.0 to 1.0, minimum 0.7) and a reason.\n\n")
	b.WriteString("Memories:\n\n")

	for i, sr := range candidates {
		if sr.Memory == nil {
			continue
		}
		m := sr.Memory
		b.WriteString(fmt.Sprintf("[%d] ID: %s\n", i+1, m.ID))
		b.WriteString(fmt.Sprintf("    Type: %s | Verdict: %s | Importance: %d | Tier: %d\n",
			m.MemoryType, m.Verdict, m.Importance, m.Tier))
		b.WriteString(fmt.Sprintf("    Content: %s\n\n", truncateContent(m.Content, 200)))
	}

	b.WriteString("\nRespond ONLY with valid JSON in this format:\n")
	b.WriteString(`{"actions": [{"type": "update_verdict", "target_id": "...", "new_verdict": "fixed", "reason": "...", "confidence": 0.85}]}`)

	return b.String()
}

type dreamerResponse struct {
	Actions []types.DreamAction `json:"actions"`
}

// validateAction checks whether a proposed action is valid.
func (d *DreamerWorker) validateAction(action types.DreamAction) bool {
	// Confidence must be >= 0.70
	if action.Confidence < 0.70 {
		return false
	}

	switch action.Type {
	case "update_verdict":
		// Check protected verdicts
		newVerdict := types.MemoryVerdict(action.NewVerdict)
		if newVerdict.IsProtected() {
			// Only allow setting protected verdicts, not changing them
			return true
		}
		// Skip if target memory has a protected verdict (checked at apply time)
		return true

	case "adjust_importance":
		if action.Delta < -3 || action.Delta > 3 {
			return false
		}
		return true

	case "merge":
		if len(action.TargetIDs) < 2 {
			return false
		}
		return true

	case "remove":
		// Tier must be >= 2 for removal
		return true

	default:
		return false
	}
}

// applyAction applies a validated dreamer action.
func (d *DreamerWorker) applyAction(ctx context.Context, tenantID string, action types.DreamAction) error {
	ctx = context.WithValue(ctx, "actor", "dreamer")

	switch action.Type {
	case "update_verdict":
		return d.applyVerdictUpdate(ctx, tenantID, action)

	case "adjust_importance":
		return d.applyImportanceAdjust(ctx, tenantID, action)

	case "merge":
		return d.applyMerge(ctx, tenantID, action)

	case "remove":
		return d.applyRemove(ctx, tenantID, action)

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func (d *DreamerWorker) applyVerdictUpdate(ctx context.Context, tenantID string, action types.DreamAction) error {
	mem, err := d.repo.GetByID(ctx, tenantID, action.TargetID)
	if err != nil {
		return fmt.Errorf("get memory failed: %w", err)
	}

	// Skip protected verdicts
	if mem.Verdict.IsProtected() {
		return fmt.Errorf("protected verdict: %s", mem.Verdict)
	}

	mem.Verdict = types.MemoryVerdict(action.NewVerdict)
	mem.UpdatedAt = time.Now()
	return d.repo.Update(ctx, mem)
}

func (d *DreamerWorker) applyImportanceAdjust(ctx context.Context, tenantID string, action types.DreamAction) error {
	mem, err := d.repo.GetByID(ctx, tenantID, action.TargetID)
	if err != nil {
		return fmt.Errorf("get memory failed: %w", err)
	}

	newImportance := mem.Importance + action.Delta
	if newImportance < -5 {
		newImportance = -5
	}
	if newImportance > 6 {
		newImportance = 6
	}
	mem.Importance = newImportance
	mem.UpdatedAt = time.Now()
	return d.repo.Update(ctx, mem)
}

func (d *DreamerWorker) applyMerge(ctx context.Context, tenantID string, action types.DreamAction) error {
	if len(action.TargetIDs) < 2 {
		return fmt.Errorf("need at least 2 targets for merge")
	}

	primary, err := d.repo.GetByID(ctx, tenantID, action.TargetIDs[0])
	if err != nil {
		return fmt.Errorf("get primary memory failed: %w", err)
	}

	var mergedContent string
	for _, id := range action.TargetIDs {
		other, err := d.repo.GetByID(ctx, tenantID, id)
		if err != nil {
			continue
		}
		if other.ID == primary.ID {
			mergedContent = other.Content
			continue
		}
		mergedContent += "\n---\n" + other.Content
	}

	if len(mergedContent) > 2000 {
		mergedContent = mergedContent[:2000]
	}
	primary.Content = mergedContent
	primary.UpdatedAt = time.Now()

	if err := d.repo.Update(ctx, primary); err != nil {
		return err
	}

	// Soft-delete the merged-away memories
	for _, id := range action.TargetIDs[1:] {
		_ = d.repo.Delete(ctx, tenantID, id)
	}

	return nil
}

func (d *DreamerWorker) applyRemove(ctx context.Context, tenantID string, action types.DreamAction) error {
	mem, err := d.repo.GetByID(ctx, tenantID, action.TargetID)
	if err != nil {
		return fmt.Errorf("get memory failed: %w", err)
	}

	// Skip tier < 2 for delete
	if mem.Tier < 2 {
		return fmt.Errorf("tier %d too low for removal", mem.Tier)
	}

	return d.repo.Delete(ctx, tenantID, action.TargetID)
}

// truncateContent truncates content to at most maxChars characters.
func truncateContent(content string, maxChars int) string {
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	return string(runes[:maxChars]) + "..."
}

// parseDuration parses a duration string with a fallback default.
func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
