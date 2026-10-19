package workers

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// purposeResolver serves a different model per purpose; Chat is the legacy
// path that must not be used when ChatFor exists.
type purposeResolver struct {
	byPurpose map[types.MemoryModelPurpose]chat.Chat
	errs      map[types.MemoryModelPurpose]error
	legacy    chat.Chat
	asked     []types.MemoryModelPurpose
}

func (r *purposeResolver) Embedder(context.Context, string, string) (embedding.Embedder, string, error) {
	return nil, "", nil
}

func (r *purposeResolver) Chat(context.Context, string, string) (chat.Chat, error) {
	return r.legacy, nil
}

func (r *purposeResolver) ChatFor(_ context.Context, _, _ string, p types.MemoryModelPurpose) (chat.Chat, error) {
	r.asked = append(r.asked, p)
	return r.byPurpose[p], r.errs[p]
}

func TestDreamer_UsesDreamerPurposeModel(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	dreamerLLM := &scriptedChat{reply: `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`}
	legacy := &scriptedChat{reply: `{"actions":[]}`}
	r := &purposeResolver{byPurpose: map[types.MemoryModelPurpose]chat.Chat{types.MemoryModelPurposeDreamer: dreamerLLM}, legacy: legacy}
	app := &recordingApplier{}

	res, err := newDreamer(store, nil, app).WithResolver(r).RunScopePass(context.Background(), "1", "kb")

	require.NoError(t, err)
	assert.Equal(t, []types.MemoryModelPurpose{types.MemoryModelPurposeDreamer}, r.asked)
	assert.Equal(t, 1, res.ActionsApplied)
	assert.Empty(t, legacy.prompt, "the summary-model path is not used")
}

func TestDreamer_UnavailableConfiguredModelFailsClosed(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	legacy := &scriptedChat{reply: `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`}
	r := &purposeResolver{errs: map[types.MemoryModelPurpose]error{types.MemoryModelPurposeDreamer: types.ErrMemoryModelUnavailable}, legacy: legacy}
	app := &recordingApplier{}

	res, err := newDreamer(store, nil, app).WithResolver(r).RunScopePass(context.Background(), "1", "kb")

	require.ErrorIs(t, err, types.ErrMemoryModelUnavailable)
	assert.Zero(t, res.ActionsApplied)
	assert.Empty(t, app.patches)
	assert.Empty(t, legacy.prompt, "no fallback to a model nobody chose for the Dreamer")
	assert.Empty(t, store.marked)
	assert.Contains(t, metricText(t), `weknora_memory_worker_actions_total{outcome="blocked_model_unavailable",worker="dreamer"}`)
}

func metricText(t *testing.T) string {
	t.Helper()
	families, err := telemetry.Registry.Gather()
	require.NoError(t, err)
	var b strings.Builder
	for _, f := range families {
		for _, m := range f.GetMetric() {
			labels := make([]string, 0, len(m.GetLabel()))
			for _, l := range m.GetLabel() {
				labels = append(labels, l.GetName()+`="`+l.GetValue()+`"`)
			}
			b.WriteString(f.GetName() + "{" + strings.Join(labels, ",") + "}\n")
		}
	}
	return b.String()
}

func TestDreamer_EachApplyingPassHasItsOwnRunID(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1), dmem("b", types.VerdictNone, 1))
	llm := &scriptedChat{reply: `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`}
	app := &recordingApplier{}
	d := newDreamer(store, llm, app)

	first, err := d.RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	second, err := d.RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.NotEmpty(t, first.RunID)
	assert.NotEqual(t, first.RunID, second.RunID)
	assert.Equal(t, first.RunID, store.released[0]["last_change_run"], "the run id is kept in the scope stats")

	llm.reply = `{"actions":[]}`
	store.state[key("1", "kb")].Stats = []byte(`{"last_change_run":"` + second.RunID + `"}`)
	quiet, err := d.RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Empty(t, quiet.RunID)
	assert.Equal(t, second.RunID, store.released[2]["last_change_run"], "a pass that changes nothing keeps the last undoable pass")

	preview, err := d.PreviewScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Empty(t, preview.RunID, "a preview changes nothing, so it has nothing to undo")
}

// lengthChat returns a reply cut off by the completion budget.
type lengthChat struct {
	scriptedChat
	content string
}

func (l *lengthChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Content: l.content, FinishReason: "length", Usage: types.TokenUsage{TotalTokens: 9000}}, nil
}

func TestDreamer_TruncatedReplyFailsAndKeepsBatchEligible(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	app := &recordingApplier{}
	d := newDreamer(store, &lengthChat{content: `{"actions":[{"type":"update_verdict","index":1,`}, app)
	_, err := d.RunScopePass(context.Background(), "1", "kb")

	require.ErrorContains(t, err, "truncated")
	assert.Empty(t, app.patches)
	assert.Empty(t, store.marked, "a truncated reply never marks the batch reviewed")
	assert.Empty(t, store.released, "and does not count as a completed run")
	assert.GreaterOrEqual(t, d.budget["1"].used, 9000, "the provider-reported usage, reasoning included, is charged")
}

func TestDreamer_CompleteAnswerDespiteLengthIsUsed(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	app := &recordingApplier{}
	reply := `{"actions":[{"type":"update_verdict","index":1,"new_verdict":"wip",` + conf + `}]}`
	res, err := newDreamer(store, &lengthChat{content: reply}, app).RunScopePass(context.Background(), "1", "kb")
	require.NoError(t, err)
	assert.Equal(t, 1, res.ActionsApplied)
}
