//go:build memory_e2e

package quality

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDreamUndoAndModelHealth runs a real Dreamer pass on a fresh knowledge
// base, undoes it through the API while a human edit made after the pass is
// kept, then deletes the chosen Dreamer model and checks that Health reports
// it and a manual pass fails closed.
func TestDreamUndoAndModelHealth(t *testing.T) {
	if os.Getenv("MEMV2_E2E_DREAM_UNDO") != "1" {
		t.Skip("set MEMV2_E2E_DREAM_UNDO=1 (with MEMV2_E2E_QUALITY_REMOTE/_MODEL/_DREAMER_MODEL)")
	}
	remote, model, dreamerModel := os.Getenv("MEMV2_E2E_QUALITY_REMOTE"), os.Getenv("MEMV2_E2E_QUALITY_MODEL"), os.Getenv("MEMV2_E2E_QUALITY_DREAMER_MODEL")
	require.NotEmpty(t, remote)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	world, err := rest.LoadIsolatedWorld(ctx)
	require.NoError(t, err)
	embedURL := "http://localhost:11434/v1"
	if world.BaseURL == "http://localhost:18082" {
		embedURL = "http://host.docker.internal:11434/v1"
	}
	name := "dream-undo-" + uuid.NewString()[:8]
	session, err := rest.Register(ctx, world.BaseURL, name, name+"@memv2-e2e.test", "Memv2-e2e!")
	require.NoError(t, err)
	client := session.As(world.BaseURL)
	provider := os.Getenv("MEMV2_E2E_QUALITY_PROVIDER")
	embedID, err := client.CreateModel(ctx, rest.ModelSpec{Name: "bge-m3", Type: "Embedding", BaseURL: embedURL, APIKey: "e2e", Dimension: 1024})
	require.NoError(t, err)
	chatURL, chatKey := remoteProvider(t, remote, model)
	chatID, err := client.CreateModel(ctx, rest.ModelSpec{Name: model, Type: "KnowledgeQA", BaseURL: chatURL, APIKey: chatKey, Provider: provider})
	require.NoError(t, err)
	dreamURL, dreamKey := remoteProvider(t, remote, dreamerModel)
	dreamerID, err := client.CreateModel(ctx, rest.ModelSpec{Name: dreamerModel, Type: "KnowledgeQA", BaseURL: dreamURL, APIKey: dreamKey, Provider: provider})
	require.NoError(t, err)
	kb, err := client.CreateKnowledgeBase(ctx, "memv2-dream-undo-"+name, embedID, chatID)
	require.NoError(t, err)
	require.NoError(t, client.SetMemoryModelSettings(ctx, kb.ID, dreamerID, ""))
	all := url.Values{"kb_id": {kb.ID}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if rows, err := client.ListMemories(cleanup, all); err == nil {
			for _, m := range rows.Items {
				_ = client.DeleteMemory(cleanup, m.ID)
			}
		}
		_ = client.Do(cleanup, http.MethodDelete, "/api/v1/knowledge-bases/"+kb.ID, nil, nil)
	}()

	// Clear verdict/importance opportunities (labelled positives) plus quiet facts.
	var cases []Case
	positives := 0
	for _, tc := range Corpus() {
		if tc.Language != "en" || tc.Kind == "merge" || tc.Kind == "importance" {
			continue
		}
		if tc.Positive && positives < 4 {
			cases, positives = append(cases, tc), positives+1
		} else if !tc.Positive && len(cases) < 12 {
			cases = append(cases, tc)
		}
	}
	for _, tc := range cases {
		saved, err := client.SaveMemory(ctx, kb.ID, tc.Content)
		require.NoError(t, err)
		_, err = client.PatchMemory(ctx, saved.Memory.ID, map[string]any{
			"verdict": tc.Verdict, "importance": tc.Importance, "tier": 1, "expected_updated_at": saved.Memory.UpdatedAt,
		})
		require.NoError(t, err)
	}
	snapshot := func() map[string]rest.Memory {
		rows, err := client.ListMemories(ctx, all)
		require.NoError(t, err)
		out := map[string]rest.Memory{}
		for _, m := range rows.Items {
			out[m.ID] = m
		}
		return out
	}
	before := snapshot()

	var dream struct{ Data types.DreamResult }
	for attempt := 0; attempt < 5; attempt++ {
		err = dreamClient(client).Do(ctx, http.MethodPost, "/api/v1/memories/dream", map[string]any{"kb_id": kb.ID}, &dream)
		if err == nil || !strings.Contains(err.Error(), "dreamer: LLM call failed") {
			break
		}
		time.Sleep(10 * time.Second)
	}
	require.NoError(t, err)
	t.Logf("dream pass: proposed=%d applied=%d tokens=%d", dream.Data.ActionsProposed, dream.Data.ActionsApplied, dream.Data.TokenUsed)
	require.GreaterOrEqual(t, dream.Data.ActionsApplied, 2, "the corpus positives must give at least two changes to undo")
	require.NotEmpty(t, dream.Data.RunID)

	var status struct{ Data types.DreamStatus }
	require.NoError(t, client.Do(ctx, http.MethodGet, "/api/v1/memories/dream?kb_id="+kb.ID, nil, &status))
	assert.Equal(t, dream.Data.RunID, status.Data.LastStats["last_change_run"], "the UI finds the pass to undo in the status")

	afterPass := snapshot()
	var changed []string
	for id, m := range afterPass {
		if b := before[id]; b.Verdict != m.Verdict || b.Importance != m.Importance {
			changed = append(changed, id)
		}
	}
	require.Len(t, changed, dream.Data.ActionsApplied)
	// A human edits one changed memory after the pass: the undo must keep it.
	kept := afterPass[changed[0]]
	_, err = client.PatchMemory(ctx, kept.ID, map[string]any{"verdict": "decision", "expected_updated_at": kept.UpdatedAt})
	require.NoError(t, err)

	var undo struct{ Data types.DreamUndoResult }
	require.NoError(t, client.Do(ctx, http.MethodPost, "/api/v1/memories/dream/undo", map[string]any{"kb_id": kb.ID, "run_id": dream.Data.RunID}, &undo))
	t.Logf("undo: reverted=%d skipped=%v", undo.Data.Reverted, undo.Data.Skipped)
	assert.Equal(t, len(changed)-1, undo.Data.Reverted)
	require.Len(t, undo.Data.Skipped, 1)
	assert.Equal(t, kept.ID, undo.Data.Skipped[0].MemoryID)
	assert.Equal(t, types.DreamUndoSkipChanged, undo.Data.Skipped[0].Reason)

	final := snapshot()
	for _, id := range changed[1:] {
		assert.Equal(t, before[id].Verdict, final[id].Verdict, id)
		assert.Equal(t, before[id].Importance, final[id].Importance, id)
		assert.Equal(t, before[id].Content, final[id].Content, id)
	}
	assert.Equal(t, "decision", string(final[kept.ID].Verdict), "the later human edit is kept")
	require.NoError(t, client.Do(ctx, http.MethodPost, "/api/v1/memories/dream/undo", map[string]any{"kb_id": kb.ID, "run_id": dream.Data.RunID}, &undo))
	assert.Zero(t, undo.Data.Reverted, "undo is idempotent")

	// The chosen Dreamer model disappears: Health says so, a pass fails closed.
	require.NoError(t, client.Do(ctx, http.MethodDelete, "/api/v1/models/"+dreamerID, nil, nil))
	var health struct{ Data types.HealthReport }
	require.NoError(t, client.Do(ctx, http.MethodGet, "/api/v1/memories/health?kb_id="+kb.ID, nil, &health))
	found := false
	for _, issue := range health.Data.Issues {
		found = found || (issue.Type == "dreamer_model_unavailable" && issue.Severity == "high")
	}
	assert.True(t, found, "Health reports the unusable Dreamer model")
	err = client.Do(ctx, http.MethodPost, "/api/v1/memories/dream", map[string]any{"kb_id": kb.ID}, nil)
	assert.Equal(t, http.StatusServiceUnavailable, rest.StatusOf(err))
	require.NoError(t, client.SetMemoryModelSettings(ctx, kb.ID, "", ""))
	require.NoError(t, client.Do(ctx, http.MethodGet, "/api/v1/memories/health?kb_id="+kb.ID, nil, &health))
	for _, issue := range health.Data.Issues {
		assert.NotEqual(t, "dreamer_model_unavailable", issue.Type, "clearing the choice restores the KB summary model")
	}
}
