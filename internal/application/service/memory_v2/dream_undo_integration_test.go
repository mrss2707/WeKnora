package memory_v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dreamByContent answers one pass with the given actions, addressing
// memories by the content prefix the prompt shows for each index.
func dreamByContent(actions ...string) *mockChat {
	return &mockChat{chatFunc: func(_ context.Context, msgs []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
		lines := strings.Split(msgs[len(msgs)-1].Content, "\n")
		idx := func(prefix string) int {
			for i := 0; i+1 < len(lines); i++ {
				var n int
				if _, err := fmt.Sscanf(lines[i], "[%d]", &n); err == nil && strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), `content="`), prefix) {
					return n
				}
			}
			return 0
		}
		out := make([]string, 0, len(actions))
		for _, a := range actions {
			parts := strings.SplitN(a, "|", 2) // "<content prefix>|<action json with %d>"
			out = append(out, fmt.Sprintf(parts[1], idx(parts[0])))
		}
		return &types.ChatResponse{Content: `{"actions":[` + strings.Join(out, ",") + `]}`}, nil
	}}
}

func TestUndoDreamRun_RevertsOnlyThatPassAndKeepsLaterEdits(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)

	a := createPGServiceMemory(t, db, "1", "kb", "A deploys on Friday failed twice", []float32{1, 0})
	b := createPGServiceMemory(t, db, "1", "kb", "B the staging cluster runs on three nodes", []float32{0, 1})
	c := createPGServiceMemory(t, db, "1", "kb", "C finish the migration runbook", []float32{1, 1})
	other := createPGServiceMemory(t, db, "1", "kb-2", "D another knowledge base", []float32{1, 0})

	first := workers.NewDreamerWorker(repo, dreamByContent(
		`A |{"type":"update_verdict","index":%d,"new_verdict":"gotcha","confidence":0.95,"reason":"pitfall"}`,
		`B |{"type":"adjust_importance","index":%d,"delta":1,"confidence":0.9,"reason":"useful"}`,
		`C |{"type":"update_verdict","index":%d,"new_verdict":"wip","confidence":0.9,"reason":"unfinished"}`,
	), cfg.Dreamer, nil).WithApplier(svc)
	res, err := first.RunScopePass(ctx, "1", "kb")
	require.NoError(t, err)
	require.Equal(t, 3, res.ActionsApplied)
	require.NotEmpty(t, res.RunID)

	// A human edits C after the pass; the undo must not overwrite it.
	gotC, err := repo.GetByID(ctx, "1", c.ID)
	require.NoError(t, err)
	decided := types.VerdictDecision
	_, err = svc.PatchMemory(ctx, "1", c.ID, interfaces.MemoryV2Patch{Verdict: &decided, ExpectedUpdatedAt: &gotC.UpdatedAt})
	require.NoError(t, err)

	_, err = svc.UndoDreamRun(ctx, "1", "kb-2", res.RunID)
	require.Error(t, err, "a pass is undone only in its own knowledge base")

	undo, err := svc.UndoDreamRun(ctx, "1", "kb", res.RunID)
	require.NoError(t, err)
	assert.Equal(t, 2, undo.Reverted)
	require.Len(t, undo.Skipped, 1)
	assert.Equal(t, types.DreamUndoSkip{MemoryID: c.ID, Action: types.MemoryAuditPatched, Reason: types.DreamUndoSkipChanged}, undo.Skipped[0])

	gotA, err := repo.GetByID(ctx, "1", a.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictNone, gotA.Verdict)
	gotB, err := repo.GetByID(ctx, "1", b.ID)
	require.NoError(t, err)
	assert.Equal(t, b.Importance, gotB.Importance)
	gotC, err = repo.GetByID(ctx, "1", c.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictDecision, gotC.Verdict, "the later human decision stays")
	gotOther, err := repo.GetByID(ctx, "1", other.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictNone, gotOther.Verdict)

	// The reverting entries are audited as the user, tagged with the pass.
	entries, _, err := repo.ListAudit(ctx, types.MemoryAuditFilter{TenantID: "1", KbID: "kb", MemoryID: a.ID, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, "user", entries[0].Actor)
	var details map[string]interface{}
	require.NoError(t, json.Unmarshal(entries[0].Details, &details))
	assert.Equal(t, res.RunID, details["undo_dream_run"])
	assert.NotContains(t, details, "dream_run")

	again, err := svc.UndoDreamRun(ctx, "1", "kb", res.RunID)
	require.NoError(t, err)
	assert.Zero(t, again.Reverted, "a second undo reverts nothing")
	reasons := map[string]string{}
	for _, s := range again.Skipped {
		reasons[s.MemoryID] = s.Reason
	}
	assert.Equal(t, map[string]string{a.ID: types.DreamUndoSkipAlreadyDone, b.ID: types.DreamUndoSkipAlreadyDone, c.ID: types.DreamUndoSkipChanged}, reasons)

	// A later pass is undone independently of the first.
	second := workers.NewDreamerWorker(repo, dreamByContent(
		`A |{"type":"update_verdict","index":%d,"new_verdict":"wip","confidence":0.95,"reason":"todo"}`,
	), cfg.Dreamer, nil).WithApplier(svc)
	res2, err := second.RunScopePass(ctx, "1", "kb")
	require.NoError(t, err)
	require.Equal(t, 1, res2.ActionsApplied)
	require.NotEqual(t, res.RunID, res2.RunID)
	undo2, err := svc.UndoDreamRun(ctx, "1", "kb", res2.RunID)
	require.NoError(t, err)
	assert.Equal(t, 1, undo2.Reverted)
	gotA, err = repo.GetByID(ctx, "1", a.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictNone, gotA.Verdict)
}

func TestUndoDreamRun_UndoesMerges(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	cfg.Dreamer.MergeLiveAfterRuns = 1
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)
	svc.embedder = &mockEmbedder{embedFunc: func(context.Context, string) ([]float32, error) { return []float32{1, 0}, nil }}

	survivor := createPGServiceMemory(t, db, "1", "kb", "S the API uses port 8080", []float32{1, 0})
	loser := createPGServiceMemory(t, db, "1", "kb", "L the API listens on port 8080", []float32{1, 0})
	model := dreamByContent(`S |{"type":"merge","indexes":[%d,LOSER],"confidence":0.95,"reason":"same fact"}`)
	byContent := model.chatFunc
	loserIndex := dreamByContent(`L |%d`)
	model.chatFunc = func(c context.Context, msgs []chat.Message, o *chat.ChatOptions) (*types.ChatResponse, error) {
		resp, _ := byContent(c, msgs, o)
		l, _ := loserIndex.chatFunc(c, msgs, o)
		n := strings.TrimSuffix(strings.TrimPrefix(l.Content, `{"actions":[`), `]}`)
		return &types.ChatResponse{Content: strings.Replace(resp.Content, "LOSER", n, 1)}, nil
	}
	state := &types.DreamerState{TenantID: "1", KbID: "kb", Runs: 1}
	require.NoError(t, db.Create(state).Error)

	res, err := workers.NewDreamerWorker(repo, model, cfg.Dreamer, nil).WithApplier(svc).RunScopePass(ctx, "1", "kb")
	require.NoError(t, err)
	require.Equal(t, 1, res.ActionsApplied)
	_, err = repo.GetByID(ctx, "1", loser.ID)
	require.Error(t, err, "the loser is merged away")

	undo, err := svc.UndoDreamRun(ctx, "1", "kb", res.RunID)
	require.NoError(t, err)
	assert.Equal(t, 1, undo.Reverted)
	assert.Empty(t, undo.Skipped, "the loser row is restored with its survivor")
	restored, err := repo.GetByID(ctx, "1", survivor.ID)
	require.NoError(t, err)
	assert.Equal(t, survivor.Content, restored.Content)
	_, err = repo.GetByID(ctx, "1", loser.ID)
	require.NoError(t, err, "the loser is back")
}
