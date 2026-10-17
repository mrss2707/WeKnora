package workers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// dreamStore is an in-memory repository with every capability the dreamer
// uses: scope lock, scope listing, batch loading, pair similarity.
type dreamStore struct {
	interfaces.MemoryRepositoryV2 // unused methods panic if called

	mu        sync.Mutex
	mems      map[string]*types.AgentMemory
	order     []string
	scopes    []Scope
	state     map[string]*types.DreamerState
	lockedBy  map[string]string
	changed   bool
	sim       map[[2]string]float64
	marked    []string
	released  []map[string]interface{}
	lockCalls int
}

func newDreamStore(mems ...*types.AgentMemory) *dreamStore {
	s := &dreamStore{mems: map[string]*types.AgentMemory{}, state: map[string]*types.DreamerState{},
		lockedBy: map[string]string{}, changed: true, sim: map[[2]string]float64{}}
	seen := map[Scope]bool{}
	for _, m := range mems {
		s.mems[m.ID] = m
		s.order = append(s.order, m.ID)
		sc := Scope{TenantID: m.TenantID, KbID: m.KbID}
		if !seen[sc] {
			seen[sc] = true
			s.scopes = append(s.scopes, sc)
		}
	}
	return s
}

func key(t, k string) string { return t + "/" + k }

func (s *dreamStore) ListMemoryScopes(context.Context) ([]types.MemoryScope, error) {
	return s.scopes, nil
}

func (s *dreamStore) TryScopeLock(_ context.Context, t, k, w string, _ time.Duration) (bool, *types.DreamerState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockCalls++
	if holder := s.lockedBy[key(t, k)]; holder != "" && holder != w {
		return false, &types.DreamerState{}, nil
	}
	s.lockedBy[key(t, k)] = w
	st, ok := s.state[key(t, k)]
	if !ok {
		st = &types.DreamerState{TenantID: t, KbID: k}
		s.state[key(t, k)] = st
	}
	cp := *st
	return true, &cp, nil
}

func (s *dreamStore) HeartbeatScopeLock(context.Context, string, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (s *dreamStore) ReleaseScopeLock(_ context.Context, t, k, w string, ran bool, stats map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lockedBy[key(t, k)] == w {
		delete(s.lockedBy, key(t, k))
	}
	if ran {
		st := s.state[key(t, k)]
		st.Runs++
		now := time.Now()
		st.LastRunAt = &now
		s.released = append(s.released, stats)
	}
	return nil
}

func (s *dreamStore) ScopeChangedSince(_ context.Context, _, _ string, t *time.Time) (bool, error) {
	return t == nil || s.changed, nil
}

func (s *dreamStore) LeastRecentlyDreamed(_ context.Context, t, k string, limit int) ([]*types.AgentMemory, error) {
	var out []*types.AgentMemory
	for _, id := range s.order {
		m := s.mems[id]
		if m != nil && m.TenantID == t && m.KbID == k && len(out) < limit {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *dreamStore) MarkDreamed(_ context.Context, _ string, ids []string, _ time.Time) error {
	s.marked = append(s.marked, ids...)
	return nil
}

func (s *dreamStore) GetByID(_ context.Context, t, id string) (*types.AgentMemory, error) {
	m := s.mems[id]
	if m == nil || m.TenantID != t {
		return nil, errors.New("not found")
	}
	cp := *m
	return &cp, nil
}

func (s *dreamStore) PairSimilarity(_ context.Context, _ string, a, b string) (float64, error) {
	if v, ok := s.sim[[2]string{a, b}]; ok {
		return v, nil
	}
	return s.sim[[2]string{b, a}], nil
}

// recordingApplier records the changes the dreamer asks the service to make.
type recordingApplier struct {
	patches []string
	merges  []string
	deletes []string
	err     error
}

func (a *recordingApplier) PatchMemory(_ context.Context, _, id string, p interfaces.MemoryV2Patch) (*types.AgentMemory, error) {
	switch {
	case p.Verdict != nil:
		a.patches = append(a.patches, fmt.Sprintf("%s verdict=%s", id, *p.Verdict))
	case p.Importance != nil:
		a.patches = append(a.patches, fmt.Sprintf("%s importance=%d", id, *p.Importance))
	}
	if p.ExpectedUpdatedAt == nil {
		a.patches = append(a.patches, "MISSING OPTIMISTIC LOCK")
	}
	return &types.AgentMemory{ID: id}, a.err
}

func (a *recordingApplier) MergeInto(_ context.Context, s *types.AgentMemory, l []*types.AgentMemory) (*types.AgentMemory, error) {
	a.merges = append(a.merges, s.ID+"<-"+l[0].ID)
	return s, a.err
}

func (a *recordingApplier) DeleteMemory(_ context.Context, _, id string) error {
	a.deletes = append(a.deletes, id)
	return a.err
}

// scriptedChat replies with a fixed text and records the prompt.
type scriptedChat struct {
	reply  string
	err    error
	prompt string
	opts   *chat.ChatOptions
	calls  int
}

func (c *scriptedChat) Chat(_ context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	c.prompt, c.opts = msgs[len(msgs)-1].Content, opts
	return &types.ChatResponse{Content: c.reply}, c.err
}
func (c *scriptedChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}
func (c *scriptedChat) GetModelName() string { return "scripted" }
func (c *scriptedChat) GetModelID() string   { return "scripted" }

func dmem(id string, v types.MemoryVerdict, importance int) *types.AgentMemory {
	past := time.Now().Add(-time.Hour)
	return &types.AgentMemory{ID: id, TenantID: "1", KbID: "kb", Content: "memory " + id,
		MemoryType: "fact", Verdict: v, Importance: importance, Tier: 2, CreatedAt: past, UpdatedAt: past}
}

func newDreamer(store *dreamStore, c chat.Chat, a DreamApplier, mut ...func(*types.DreamerConfig)) *DreamerWorker {
	cfg := types.DefaultMemoryV2Config().Dreamer
	for _, m := range mut {
		m(&cfg)
	}
	d := NewDreamerWorker(store, c, cfg, nil)
	if a != nil {
		d.WithApplier(a)
	}
	return d
}

const conf = `"confidence":0.95,"reason":"r"`

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDreamer_AppliesVerdictAndImportanceWithLocks(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1), dmem("b", types.VerdictNone, 6))
	llm := &scriptedChat{reply: "```json\n{\"actions\":[" +
		`{"type":"update_verdict","index":1,"new_verdict":"gotcha",` + conf + `},` +
		`{"type":"adjust_importance","index":2,"delta":1,` + conf + `}` + "]}\n```"}
	app := &recordingApplier{}

	res, err := newDreamer(store, llm, app).RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Equal(t, []string{"a verdict=gotcha"}, app.patches, "importance already 6: +1 is skipped")
	assert.Equal(t, 2, res.ActionsProposed)
	assert.Equal(t, 1, res.ActionsApplied)
	assert.Zero(t, llm.opts.Temperature, "deterministic")
	assert.ElementsMatch(t, []string{"a", "b"}, store.marked, "the batch is marked reviewed")
	assert.NotContains(t, llm.prompt, "\"a\"", "the model sees indexes, never database ids")
}

func TestDreamer_NeverTouchesProtectedVerdicts(t *testing.T) {
	store := newDreamStore(dmem("d", types.VerdictDecision, 3), dmem("f", types.VerdictFixed, 3), dmem("n", types.VerdictNone, 3))
	llm := &scriptedChat{reply: `{"actions":[` +
		`{"type":"update_verdict","index":1,"new_verdict":"refuted",` + conf + `},` +
		`{"type":"adjust_importance","index":2,"delta":-1,` + conf + `},` +
		`{"type":"update_verdict","index":3,"new_verdict":"decision",` + conf + `},` +
		`{"type":"update_verdict","index":3,"new_verdict":"fixed",` + conf + `}]}`}
	app := &recordingApplier{}

	res, err := newDreamer(store, llm, app).RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Empty(t, app.patches, "decision/fixed can neither be changed nor be set by the dreamer")
	assert.Zero(t, res.ActionsProposed)
	assert.Contains(t, llm.prompt, "[protected]")
}

func TestDreamer_RejectsInvalidProposals(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	llm := &scriptedChat{reply: `{"actions":[` +
		`{"type":"update_verdict","index":9,"new_verdict":"gotcha",` + conf + `},` +
		`{"type":"update_verdict","index":1,"new_verdict":"bogus",` + conf + `},` +
		`{"type":"update_verdict","index":1,"new_verdict":"none",` + conf + `},` +
		`{"type":"adjust_importance","index":1,"delta":3,` + conf + `},` +
		`{"type":"adjust_importance","index":1,"delta":1,"confidence":0.5},` +
		`{"type":"remove","index":1,` + conf + `},` +
		`{"type":"explode","index":1,` + conf + `}]}`}
	app := &recordingApplier{}

	res, err := newDreamer(store, llm, app).RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Zero(t, res.ActionsProposed, "out-of-range index, unknown verdict, no-op, big delta, low confidence, remove (off), unknown type")
	assert.Empty(t, app.patches)
	assert.Empty(t, app.deletes)
}

func TestDreamer_GarbageRepliesProposeNothing(t *testing.T) {
	for _, reply := range []string{"", "I think all memories look fine.", "{not json", `{"actions": "x"}`} {
		store := newDreamStore(dmem("a", types.VerdictNone, 1))
		res, err := newDreamer(store, &scriptedChat{reply: reply}, &recordingApplier{}).RunScopePass(context.Background(), "1", "kb")
		require.NoError(t, err, reply)
		assert.Zero(t, res.ActionsProposed, reply)
	}
}

func TestDreamer_CircuitBreakerAbortsMassChanges(t *testing.T) {
	var mems []*types.AgentMemory
	var acts []string
	for i := 1; i <= 9; i++ {
		mems = append(mems, dmem(fmt.Sprintf("m%d", i), types.VerdictNone, 0))
		acts = append(acts, fmt.Sprintf(`{"type":"update_verdict","index":%d,"new_verdict":"refuted",%s}`, i, conf))
	}
	store := newDreamStore(mems...)
	app := &recordingApplier{}

	res, err := newDreamer(store, &scriptedChat{reply: `{"actions":[` + strings.Join(acts, ",") + `]}`}, app).
		RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Empty(t, app.patches, "9 of 9 memories refuted at once is not trusted")
	assert.Zero(t, res.ActionsApplied)
	require.Len(t, store.released, 1)
	assert.Contains(t, store.released[0]["aborted"], "circuit breaker")
}

func TestDreamer_MergeIsProbationaryAndServerVerified(t *testing.T) {
	reply := `{"actions":[{"type":"merge","indexes":[1,2],` + conf + `}]}`
	setup := func(runs int, sim float64) (*dreamStore, *recordingApplier, *types.DreamResult) {
		store := newDreamStore(dmem("a", types.VerdictNone, 1), dmem("b", types.VerdictNone, 1))
		store.state[key("1", "kb")] = &types.DreamerState{Runs: runs}
		store.sim[[2]string{"a", "b"}] = sim
		app := &recordingApplier{}
		res, err := newDreamer(store, &scriptedChat{reply: reply}, app).RunScopePass(context.Background(), "1", "kb")
		require.NoError(t, err)
		return store, app, res
	}

	_, app, res := setup(0, 0.99)
	assert.Empty(t, app.merges, "first passes only propose merges")
	assert.Equal(t, 1, res.ActionsProposed)
	assert.Len(t, res.Actions, 1)

	_, app, _ = setup(3, 0.80)
	assert.Empty(t, app.merges, "the model's opinion is not enough: stored vectors must agree")

	_, app, res = setup(3, 0.97)
	assert.Equal(t, []string{"a<-b"}, app.merges)
	assert.Equal(t, 1, res.ActionsApplied)
}

func TestDreamer_SkipsMemoriesChangedAfterReview(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	llm := &scriptedChat{reply: `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`}
	d := newDreamer(store, llm, &recordingApplier{})
	// A user edits the memory while the model is thinking.
	store.mems["a"].UpdatedAt = time.Now().Add(time.Hour)
	app := &recordingApplier{}
	d.WithApplier(app)

	res, err := d.RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Empty(t, app.patches)
	assert.Zero(t, res.ActionsApplied)
}

func TestDreamer_DryRunAndNoApplierOnlyPropose(t *testing.T) {
	reply := `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`
	app := &recordingApplier{}
	res, err := newDreamer(newDreamStore(dmem("a", types.VerdictNone, 1)), &scriptedChat{reply: reply}, app,
		func(c *types.DreamerConfig) { c.DryRun = true }).RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Empty(t, app.patches)
	assert.Equal(t, 1, res.ActionsProposed)
	assert.Zero(t, res.ActionsApplied)

	res, err = newDreamer(newDreamStore(dmem("a", types.VerdictNone, 1)), &scriptedChat{reply: reply}, nil).
		RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Zero(t, res.ActionsApplied, "without an applier nothing is written")
}

func TestDreamer_PreviewAppliesAndRecordsNothing(t *testing.T) {
	reply := `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`
	app := &recordingApplier{}
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	res, err := newDreamer(store, &scriptedChat{reply: reply}, app).PreviewScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Equal(t, 1, res.ActionsProposed)
	assert.Zero(t, res.ActionsApplied)
	require.Len(t, res.Actions, 1, "the proposal is returned for review")
	assert.Empty(t, app.patches, "a preview never writes")
	assert.Empty(t, store.marked, "a preview does not mark memories reviewed")
	assert.Empty(t, store.released, "a preview is not counted as a run")
	assert.Empty(t, store.lockedBy, "the lock is released")
}

func TestDreamer_RemoveOnlyWhenAllowed(t *testing.T) {
	reply := `{"actions":[{"type":"remove","index":1,` + conf + `}]}`
	app := &recordingApplier{}
	newDreamer(newDreamStore(dmem("a", types.VerdictNone, 1)), &scriptedChat{reply: reply}, app).
		RunScopePass(context.Background(), "1", "kb")
	assert.Empty(t, app.deletes, "remove is off by default")

	newDreamer(newDreamStore(dmem("a", types.VerdictNone, 1)), &scriptedChat{reply: reply}, app,
		func(c *types.DreamerConfig) { c.AllowRemove = true }).RunScopePass(context.Background(), "1", "kb")
	assert.Equal(t, []string{"a"}, app.deletes)
}

func TestDreamer_ScheduledPassSkipsUnchangedScopesAndRunsPerKB(t *testing.T) {
	other := dmem("x", types.VerdictNone, 1)
	other.KbID = "kb-2"
	store := newDreamStore(dmem("a", types.VerdictNone, 1), other)
	llm := &scriptedChat{reply: `{"actions":[]}`}
	d := newDreamer(store, llm, &recordingApplier{})

	d.dreamPass(context.Background())
	assert.Equal(t, 2, llm.calls, "one pass per knowledge base")
	assert.NotContains(t, llm.prompt, "memory a", "a pass never mixes knowledge bases")

	store.changed = false
	d.dreamPass(context.Background())
	assert.Equal(t, 2, llm.calls, "nothing changed since the last pass: no model call")

	_, err := d.RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Equal(t, 3, llm.calls, "a manual trigger always runs")
}

func TestDreamer_LockHeldElsewhereSkips(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	store.lockedBy[key("1", "kb")] = "other-replica"
	llm := &scriptedChat{reply: `{"actions":[]}`}

	res, err := newDreamer(store, llm, &recordingApplier{}).RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Zero(t, llm.calls)
	assert.Zero(t, res.ActionsProposed)
	assert.Equal(t, "other-replica", store.lockedBy[key("1", "kb")], "another replica's lock is never released")
}

func TestDreamer_DailyTokenBudget(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	llm := &scriptedChat{reply: `{"actions":[]}`}
	d := newDreamer(store, llm, &recordingApplier{}, func(c *types.DreamerConfig) { c.DailyTokenBudget = 10 })

	_, _ = d.RunScopePass(context.Background(), "1", "kb")
	_, _ = d.RunScopePass(context.Background(), "1", "kb")
	assert.Equal(t, 1, llm.calls, "the second pass is over budget")

	d.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	_, _ = d.RunScopePass(context.Background(), "1", "kb")
	assert.Equal(t, 2, llm.calls, "the budget resets the next day")
}

func TestDreamer_DisabledOrNoTenantDoesNothing(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	llm := &scriptedChat{reply: `{"actions":[]}`}
	_, _ = newDreamer(store, llm, nil, func(c *types.DreamerConfig) { c.Enabled = false }).RunScopePass(context.Background(), "1", "kb")
	_, _ = newDreamer(store, llm, nil).RunScopePass(context.Background(), "", "kb")
	assert.Zero(t, llm.calls)
	assert.Zero(t, store.lockCalls)
}

func TestDreamer_ModelErrorReleasesLock(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	_, err := newDreamer(store, &scriptedChat{err: errors.New("provider down")}, nil).RunScopePass(context.Background(), "1", "kb")
	require.Error(t, err)
	assert.Empty(t, store.lockedBy, "the lock is released even when the pass fails")
	assert.Empty(t, store.marked, "a failed pass does not mark memories reviewed")
}

func TestParseDreamActions_ToleratesFencesAndProse(t *testing.T) {
	got := parseDreamActions("Sure! Here you go:\n```json\n{\"actions\":[{\"type\":\"merge\",\"indexes\":[1,2],\"confidence\":0.9}]}\n```\nDone.")
	require.Len(t, got, 1)
	assert.Equal(t, []int{1, 2}, got[0].Indexes)
}
