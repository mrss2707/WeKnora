package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DreamApplier performs the dreamer's changes through the service, so every
// change goes through the same validation, merge transaction and audit trail
// as a user's change. Implemented by the Memory V2 service.
type DreamApplier interface {
	PatchMemory(ctx context.Context, tenantID, id string, patch interfaces.MemoryV2Patch) (*types.AgentMemory, error)
	MergeInto(ctx context.Context, survivor *types.AgentMemory, losers []*types.AgentMemory) (*types.AgentMemory, error)
	DeleteMemory(ctx context.Context, tenantID, id string) error
}

// dreamerLockTTL bounds how long a crashed pass can hold a scope.
const dreamerLockTTL = 5 * time.Minute

// DreamerWorker periodically reviews each knowledge base's memories with an
// LLM and applies a small number of conservative, audited improvements:
// verdict corrections (never touching decision/fixed), importance nudges
// (+-1), and — after a probation period and a server-side similarity check —
// merges of near-duplicates. Deletion is off unless explicitly allowed.
type DreamerWorker struct {
	repo     interfaces.MemoryRepositoryV2
	chat     chat.Chat
	resolver ModelResolver
	applier  DreamApplier
	config   types.DreamerConfig
	interval time.Duration
	workerID string
	now      func() time.Time

	budgetMu sync.Mutex
	budget   map[string]dailyBudget // tenant -> tokens used today
}

type dailyBudget struct {
	day  string
	used int
}

// NewDreamerWorker creates a new DreamerWorker. The last argument is unused
// (passes enumerate the scopes holding memories); kept for compatibility.
func NewDreamerWorker(repo interfaces.MemoryRepositoryV2, chat chat.Chat, config types.DreamerConfig, _ []string) *DreamerWorker {
	return &DreamerWorker{
		repo:     repo,
		chat:     chat,
		config:   withDreamerDefaults(config),
		interval: parseDuration(config.Interval, 6*time.Hour),
		workerID: fmt.Sprintf("dreamer-%d", time.Now().UnixNano()),
		now:      time.Now,
		budget:   map[string]dailyBudget{},
	}
}

func withDreamerDefaults(c types.DreamerConfig) types.DreamerConfig {
	d := types.DefaultMemoryV2Config().Dreamer
	if c.MaxActions <= 0 {
		c.MaxActions = d.MaxActions
	}
	if c.TokenBudget <= 0 {
		c.TokenBudget = d.TokenBudget
	}
	if c.BatchSize <= 0 {
		c.BatchSize = d.BatchSize
	}
	if c.MinConfidence <= 0 {
		c.MinConfidence = d.MinConfidence
	}
	if c.MergeMinCosine <= 0 {
		c.MergeMinCosine = d.MergeMinCosine
	}
	if c.MaxChangeRatio <= 0 {
		c.MaxChangeRatio = d.MaxChangeRatio
	}
	return c
}

// WithResolver makes the dreamer resolve each scope's chat model.
func (d *DreamerWorker) WithResolver(r ModelResolver) *DreamerWorker {
	d.resolver = r
	return d
}

// WithApplier routes the dreamer's changes through the service. Without an
// applier the dreamer only proposes (nothing is written).
func (d *DreamerWorker) WithApplier(a DreamApplier) *DreamerWorker {
	d.applier = a
	return d
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

// RunPass executes one pass over every knowledge base of a tenant.
func (d *DreamerWorker) RunPass(ctx context.Context, tenantID string) (*types.DreamResult, error) {
	return d.RunScopePass(ctx, tenantID, "")
}

// RunScopePass executes one pass for a knowledge base (a manual trigger runs
// even when nothing changed since the last pass). An empty kbID runs every
// knowledge base of the tenant.
func (d *DreamerWorker) RunScopePass(ctx context.Context, tenantID, kbID string) (*types.DreamResult, error) {
	total := &types.DreamResult{Actions: []types.DreamAction{}}
	if !d.config.Enabled || tenantID == "" {
		return total, nil
	}
	scopes := []Scope{{TenantID: tenantID, KbID: kbID}}
	if kbID == "" {
		all, err := listScopes(ctx, d.repo)
		if err != nil {
			return total, err
		}
		scopes = scopes[:0]
		for _, sc := range all {
			if sc.TenantID == tenantID {
				scopes = append(scopes, sc)
			}
		}
	}
	for _, sc := range scopes {
		res, err := d.executeScopePass(ctx, sc, true)
		if err != nil {
			return total, err
		}
		total.ActionsProposed += res.ActionsProposed
		total.ActionsApplied += res.ActionsApplied
		total.TokenUsed += res.TokenUsed
		total.Actions = append(total.Actions, res.Actions...)
	}
	return total, nil
}

// dreamPass reviews every scope that changed since its last pass.
func (d *DreamerWorker) dreamPass(ctx context.Context) {
	scopes, err := listScopes(ctx, d.repo)
	if err != nil {
		logger.Errorf(ctx, "dreamer: list scopes failed: %v", err)
		return
	}
	for _, sc := range scopes {
		if ctx.Err() != nil {
			return
		}
		if sc.TenantID == "" {
			continue
		}
		res, err := d.executeScopePass(ctx, sc, false)
		if err != nil {
			logger.Errorf(ctx, "dreamer: pass failed for %s/%s: %v", sc.TenantID, sc.KbID, err)
			continue
		}
		if res.ActionsProposed > 0 {
			logger.Infof(ctx, "dreamer: %s/%s proposed=%d applied=%d tokens~%d",
				sc.TenantID, sc.KbID, res.ActionsProposed, res.ActionsApplied, res.TokenUsed)
		}
	}
}

// executeScopePass runs one pass for one scope. force skips the "changed
// since the last pass" check (manual triggers).
func (d *DreamerWorker) executeScopePass(ctx context.Context, sc Scope, force bool) (*types.DreamResult, error) {
	result := &types.DreamResult{Actions: []types.DreamAction{}}
	lock, ok := d.repo.(interfaces.MemoryV2ScopeLock)
	if !ok {
		return result, fmt.Errorf("dreamer: repository cannot lock scopes")
	}
	ctx = context.WithValue(ctx, types.ActorKey{}, "dreamer")

	acquired, state, err := lock.TryScopeLock(ctx, sc.TenantID, sc.KbID, d.workerID, dreamerLockTTL)
	if err != nil {
		return result, fmt.Errorf("dreamer: lock: %w", err)
	}
	if !acquired {
		return result, nil
	}
	ran := false
	stats := map[string]interface{}{}
	stopHeartbeat := d.heartbeat(ctx, lock, sc)
	defer func() {
		stopHeartbeat()
		if err := lock.ReleaseScopeLock(context.WithoutCancel(ctx), sc.TenantID, sc.KbID, d.workerID, ran, stats); err != nil {
			logger.Warnf(ctx, "dreamer: release lock: %v", err)
		}
	}()

	if !force {
		changed, err := lock.ScopeChangedSince(ctx, sc.TenantID, sc.KbID, state.LastRunAt)
		if err != nil || !changed {
			return result, err
		}
	}
	if !d.withinBudget(sc.TenantID) {
		stats["skipped"] = "daily token budget exhausted"
		return result, nil
	}

	batch, err := lock.LeastRecentlyDreamed(ctx, sc.TenantID, sc.KbID, d.config.BatchSize)
	if err != nil {
		return result, fmt.Errorf("dreamer: load batch: %w", err)
	}
	if len(batch) == 0 {
		return result, nil
	}
	chatModel, err := d.chatFor(ctx, sc)
	if err != nil {
		return result, err
	}

	prompt := d.buildPrompt(batch)
	readAt := d.now()
	resp, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: dreamerSystemPrompt},
		{Role: "user", Content: prompt},
	}, &chat.ChatOptions{MaxTokens: d.config.TokenBudget, Temperature: 0})
	if err != nil {
		return result, fmt.Errorf("dreamer: LLM call failed: %w", err)
	}
	ran = true
	result.TokenUsed = estimateTokens(prompt) + estimateTokens(resp.Content)
	d.spend(sc.TenantID, result.TokenUsed)

	ids := make([]string, len(batch))
	for i, m := range batch {
		ids[i] = m.ID
	}
	defer func() {
		if err := lock.MarkDreamed(context.WithoutCancel(ctx), sc.TenantID, ids, readAt); err != nil {
			logger.Warnf(ctx, "dreamer: mark reviewed: %v", err)
		}
	}()

	actions := d.resolveActions(parseDreamActions(resp.Content), batch)
	result.ActionsProposed = len(actions)
	stats["proposed"] = len(actions)

	limit := int(math.Max(3, math.Ceil(d.config.MaxChangeRatio*float64(len(batch)))))
	if len(actions) > limit {
		stats["aborted"] = fmt.Sprintf("circuit breaker: %d proposals for %d memories", len(actions), len(batch))
		logger.Warnf(ctx, "dreamer: %s/%s %s", sc.TenantID, sc.KbID, stats["aborted"])
		result.ActionsProposed = 0
		return result, nil
	}
	if len(actions) > d.config.MaxActions {
		actions = actions[:d.config.MaxActions]
	}

	mergeLive := d.config.MergeLiveAfterRuns > 0 && state.Runs >= d.config.MergeLiveAfterRuns
	for _, a := range actions {
		dry := d.config.DryRun || d.applier == nil || (a.Type == "merge" && !mergeLive)
		if !dry {
			if err := d.apply(ctx, sc, a, readAt); err != nil {
				logger.Infof(ctx, "dreamer: skipped %s on %s: %v", a.Type, a.TargetID, err)
				continue
			}
			result.ActionsApplied++
		}
		result.Actions = append(result.Actions, a)
	}
	stats["applied"] = result.ActionsApplied
	return result, nil
}

// heartbeat extends the scope lock while a pass runs (a slow model call can
// outlast the TTL); the returned func stops it.
func (d *DreamerWorker) heartbeat(ctx context.Context, lock interfaces.MemoryV2ScopeLock, sc Scope) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(dreamerLockTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if ok, err := lock.HeartbeatScopeLock(ctx, sc.TenantID, sc.KbID, d.workerID, dreamerLockTTL); err != nil || !ok {
					logger.Warnf(ctx, "dreamer: lost lock heartbeat for %s/%s: %v", sc.TenantID, sc.KbID, err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func (d *DreamerWorker) chatFor(ctx context.Context, sc Scope) (chat.Chat, error) {
	if d.resolver != nil {
		ch, err := d.resolver.Chat(ctx, sc.TenantID, sc.KbID)
		if err != nil {
			return nil, fmt.Errorf("dreamer: resolve chat model: %w", err)
		}
		return ch, nil
	}
	if d.chat == nil {
		return nil, fmt.Errorf("dreamer: no chat model for %s/%s", sc.TenantID, sc.KbID)
	}
	return d.chat, nil
}

const dreamerSystemPrompt = "You maintain a team's long-term memory. You review memories and propose only " +
	"changes you are highly confident about. Most memories need no change: an empty action list is the " +
	"usual, correct answer. Output ONLY a JSON object."

// buildPrompt lists the batch by index. The model never sees database ids, so
// it cannot invent or mistype one.
func (d *DreamerWorker) buildPrompt(batch []*types.AgentMemory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review these %d memories. Propose at most %d actions:\n", len(batch), d.config.MaxActions)
	b.WriteString("- {\"type\":\"update_verdict\",\"index\":N,\"new_verdict\":\"refuted|gotcha|wip|none\"}: " +
		"refuted = contradicted by a newer memory; gotcha = a pitfall to remember; wip = unfinished.\n")
	b.WriteString("- {\"type\":\"adjust_importance\",\"index\":N,\"delta\":1 or -1}\n")
	b.WriteString("- {\"type\":\"merge\",\"indexes\":[N,M]}: only for memories stating the same fact.\n")
	if d.config.AllowRemove {
		b.WriteString("- {\"type\":\"remove\",\"index\":N}: only for empty or meaningless memories.\n")
	}
	b.WriteString("Never change memories marked [protected]. Each action needs \"confidence\" (0..1) and a short \"reason\".\n")
	b.WriteString("Answer {\"actions\":[...]} and nothing else.\n\nMemories:\n")
	for i, m := range batch {
		flag := ""
		if m.Verdict.IsProtected() {
			flag = " [protected]"
		}
		fmt.Fprintf(&b, "[%d] type=%s verdict=%s importance=%d%s created=%s\n    %s\n",
			i+1, m.MemoryType, m.Verdict, m.Importance, flag, m.CreatedAt.Format("2006-01-02"),
			truncateContent(oneLine(m.Content), 300))
	}
	return b.String()
}

// rawDreamAction is the model's JSON for one action.
type rawDreamAction struct {
	Type       string  `json:"type"`
	Index      int     `json:"index"`
	Indexes    []int   `json:"indexes"`
	NewVerdict string  `json:"new_verdict"`
	Delta      int     `json:"delta"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

var jsonObject = regexp.MustCompile(`(?s)\{.*\}`)

// parseDreamActions extracts the actions from a model reply, tolerating code
// fences and surrounding prose. Anything unparseable yields no actions.
func parseDreamActions(reply string) []rawDreamAction {
	raw := jsonObject.FindString(reply)
	if raw == "" {
		return nil
	}
	var out struct {
		Actions []rawDreamAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out.Actions
}

// allowedDreamVerdicts are the verdicts the dreamer may set. It never sets or
// clears decision/fixed: those are human decisions.
var allowedDreamVerdicts = map[types.MemoryVerdict]bool{
	types.VerdictNone: true, types.VerdictRefuted: true, types.VerdictGotcha: true, types.VerdictWIP: true,
}

// resolveActions validates proposals against the batch and maps indexes to
// memory ids. Invalid, low-confidence, no-op and protected-target proposals
// are dropped; at most one action per memory survives.
func (d *DreamerWorker) resolveActions(raw []rawDreamAction, batch []*types.AgentMemory) []types.DreamAction {
	at := func(i int) *types.AgentMemory {
		if i < 1 || i > len(batch) {
			return nil
		}
		return batch[i-1]
	}
	touched := map[string]bool{}
	var out []types.DreamAction
	for _, r := range raw {
		if r.Confidence < d.config.MinConfidence {
			continue
		}
		switch r.Type {
		case "update_verdict":
			m := at(r.Index)
			v := types.MemoryVerdict(r.NewVerdict)
			if m == nil || touched[m.ID] || m.Verdict.IsProtected() || !allowedDreamVerdicts[v] || m.Verdict == v {
				continue
			}
			touched[m.ID] = true
			out = append(out, types.DreamAction{Type: r.Type, TargetID: m.ID, NewVerdict: string(v), Reason: r.Reason, Confidence: r.Confidence})
		case "adjust_importance":
			m := at(r.Index)
			if m == nil || touched[m.ID] || m.Verdict.IsProtected() || (r.Delta != 1 && r.Delta != -1) {
				continue
			}
			touched[m.ID] = true
			out = append(out, types.DreamAction{Type: r.Type, TargetID: m.ID, Delta: r.Delta, Reason: r.Reason, Confidence: r.Confidence})
		case "merge":
			if len(r.Indexes) != 2 {
				continue
			}
			a, b := at(r.Indexes[0]), at(r.Indexes[1])
			if a == nil || b == nil || a.ID == b.ID || touched[a.ID] || touched[b.ID] ||
				a.Verdict.IsProtected() || b.Verdict.IsProtected() || a.KbID != b.KbID {
				continue
			}
			touched[a.ID], touched[b.ID] = true, true
			out = append(out, types.DreamAction{Type: r.Type, TargetID: a.ID, TargetIDs: []string{a.ID, b.ID}, Reason: r.Reason, Confidence: r.Confidence})
		case "remove":
			m := at(r.Index)
			if !d.config.AllowRemove || m == nil || touched[m.ID] || m.Verdict.IsProtected() || m.Tier < 2 {
				continue
			}
			touched[m.ID] = true
			out = append(out, types.DreamAction{Type: r.Type, TargetID: m.ID, Reason: r.Reason, Confidence: r.Confidence})
		}
	}
	return out
}

// apply performs one validated action through the service. Memories changed
// by anyone after the batch was read are skipped.
func (d *DreamerWorker) apply(ctx context.Context, sc Scope, a types.DreamAction, readAt time.Time) error {
	fresh := func(id string) (*types.AgentMemory, error) {
		m, err := d.repo.GetByID(ctx, sc.TenantID, id)
		if err != nil {
			return nil, err
		}
		if m == nil || m.KbID != sc.KbID {
			return nil, fmt.Errorf("memory left the scope")
		}
		if m.UpdatedAt.After(readAt) {
			return nil, fmt.Errorf("changed after review")
		}
		if m.Verdict.IsProtected() {
			return nil, &types.ErrProtectedVerdict{}
		}
		return m, nil
	}
	switch a.Type {
	case "update_verdict":
		m, err := fresh(a.TargetID)
		if err != nil {
			return err
		}
		v := types.MemoryVerdict(a.NewVerdict)
		_, err = d.applier.PatchMemory(ctx, sc.TenantID, m.ID, interfaces.MemoryV2Patch{Verdict: &v, ExpectedUpdatedAt: &m.UpdatedAt})
		return err
	case "adjust_importance":
		m, err := fresh(a.TargetID)
		if err != nil {
			return err
		}
		imp := clampImportance(m.Importance + a.Delta)
		if imp == m.Importance {
			return fmt.Errorf("importance already at its bound")
		}
		_, err = d.applier.PatchMemory(ctx, sc.TenantID, m.ID, interfaces.MemoryV2Patch{Importance: &imp, ExpectedUpdatedAt: &m.UpdatedAt})
		return err
	case "merge":
		survivor, err := fresh(a.TargetIDs[0])
		if err != nil {
			return err
		}
		loser, err := fresh(a.TargetIDs[1])
		if err != nil {
			return err
		}
		pair, ok := d.repo.(interfaces.MemoryV2PairSimilarity)
		if !ok {
			return fmt.Errorf("stored vectors unavailable")
		}
		sim, err := pair.PairSimilarity(ctx, sc.TenantID, survivor.ID, loser.ID)
		if err != nil {
			return err
		}
		if sim < d.config.MergeMinCosine {
			return fmt.Errorf("not similar enough to merge (%.2f < %.2f)", sim, d.config.MergeMinCosine)
		}
		_, err = d.applier.MergeInto(ctx, survivor, []*types.AgentMemory{loser})
		return err
	case "remove":
		m, err := fresh(a.TargetID)
		if err != nil {
			return err
		}
		return d.applier.DeleteMemory(ctx, sc.TenantID, m.ID)
	}
	return fmt.Errorf("unknown action type: %s", a.Type)
}

func (d *DreamerWorker) withinBudget(tenantID string) bool {
	if d.config.DailyTokenBudget <= 0 {
		return true
	}
	d.budgetMu.Lock()
	defer d.budgetMu.Unlock()
	b := d.budget[tenantID]
	return b.day != d.now().Format("2006-01-02") || b.used < d.config.DailyTokenBudget
}

func (d *DreamerWorker) spend(tenantID string, tokens int) {
	d.budgetMu.Lock()
	defer d.budgetMu.Unlock()
	day := d.now().Format("2006-01-02")
	b := d.budget[tenantID]
	if b.day != day {
		b = dailyBudget{day: day}
	}
	b.used += tokens
	d.budget[tenantID] = b
}

// estimateTokens approximates tokens from characters (mixed scripts ~3/token).
func estimateTokens(s string) int {
	return len([]rune(s))/3 + 1
}

func clampImportance(v int) int {
	if v < -5 {
		return -5
	}
	if v > 6 {
		return 6
	}
	return v
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
