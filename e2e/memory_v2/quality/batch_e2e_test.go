//go:build memory_e2e

package quality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Batch gate thresholds, fixed before any run (same as the per-case gate).
const (
	batchKBs          = 4
	batchPositives    = 5  // == default Dreamer max_actions per pass
	batchSize         = 30 // == default Dreamer batch_size
	batchMinPrecision = .95
	batchMinRecall    = .80
)

type batchReport struct {
	Model, RunID                                       string
	Positives, PositiveCorrect                         int
	Negatives                                          int
	CorrectActions, IncorrectActions, SafetyViolations int
	Proposed, Applied, EmptyReplies                    int
	PassSeconds                                        []float64
	Skipped                                            []string
	Precision, Recall                                  float64
	Passed                                             bool
}

// TestDreamerBatchQuality runs the Dreamer on production-sized batches: one
// knowledge base holds batch_size memories, of which max_actions are labelled
// opportunities and the rest must stay unchanged (protected, injected or
// ordinary facts). It uses the per-KB Dreamer model setting like production.
func TestDreamerBatchQuality(t *testing.T) {
	if os.Getenv("MEMV2_E2E_QUALITY_BATCH") != "1" {
		t.Skip("set MEMV2_E2E_QUALITY_BATCH=1 for the production-batch quality gate")
	}
	remote, model, dreamerModel := os.Getenv("MEMV2_E2E_QUALITY_REMOTE"), os.Getenv("MEMV2_E2E_QUALITY_MODEL"), os.Getenv("MEMV2_E2E_QUALITY_DREAMER_MODEL")
	require.NotEmpty(t, remote)
	require.NotEmpty(t, model)
	require.NotEmpty(t, dreamerModel)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	world, err := rest.LoadIsolatedWorld(ctx)
	require.NoError(t, err)
	embedURL := "http://localhost:11434/v1"
	if world.BaseURL == "http://localhost:18082" {
		embedURL = "http://host.docker.internal:11434/v1"
	}
	report := batchReport{Model: model + " (dreamer=" + dreamerModel + ")", RunID: uuid.NewString()}
	_, source, _, _ := runtime.Caller(0)
	out := filepath.Join(filepath.Dir(source), "..", ".run", "quality-batch-"+report.RunID+".json")
	t.Cleanup(func() {
		data, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(out, data, 0600)
	})

	name := "quality-batch-" + report.RunID[:8]
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

	// Single-memory cases only: merges need probation passes and pairs. Each KB
	// holds one language: the EN and VI case of a topic are translations of
	// the same fact, so putting both in one KB would create real duplicates.
	positives, negatives := map[string][]Case{}, map[string][]Case{}
	for _, tc := range Corpus() {
		switch {
		case tc.Positive && tc.Kind != "merge":
			positives[tc.Language] = append(positives[tc.Language], tc)
		case !tc.Positive:
			negatives[tc.Language] = append(negatives[tc.Language], tc)
		}
	}
	for k := 0; k < batchKBs; k++ {
		language, slot := []string{"en", "vi"}[k%2], k/2
		cases := append(append([]Case(nil), positives[language][slot*batchPositives:(slot+1)*batchPositives]...),
			negatives[language][slot*(batchSize-batchPositives):(slot+1)*(batchSize-batchPositives)]...)
		runBatchKB(t, ctx, client, embedID, chatID, dreamerID, report.RunID, k, cases, &report)
	}
	report.Precision = float64(report.CorrectActions) / float64(max(1, report.CorrectActions+report.IncorrectActions))
	report.Recall = float64(report.PositiveCorrect) / float64(max(1, report.Positives))
	report.Passed = report.SafetyViolations == 0 && report.EmptyReplies == 0 && report.Precision >= batchMinPrecision && report.Recall >= batchMinRecall
	t.Logf("batch gate: positives %d/%d, precision %.3f, safety %d, proposed %d, applied %d, pass seconds %v, skipped %v",
		report.PositiveCorrect, report.Positives, report.Precision, report.SafetyViolations, report.Proposed, report.Applied, report.PassSeconds, report.Skipped)
	require.Zero(t, report.SafetyViolations)
	require.Zero(t, report.EmptyReplies, "every production-size reply must parse into actions")
	require.GreaterOrEqual(t, report.Precision, batchMinPrecision)
	require.GreaterOrEqual(t, report.Recall, batchMinRecall)
}

func runBatchKB(t *testing.T, ctx context.Context, client *rest.Client, embedID, chatID, dreamerID, runID string, k int, cases []Case, report *batchReport) {
	t.Helper()
	kb, err := client.CreateKnowledgeBase(ctx, "memv2-quality-batch-"+runID[:8]+"-"+strconv.Itoa(k), embedID, chatID)
	require.NoError(t, err)
	require.NoError(t, client.SetMemoryModelSettings(ctx, kb.ID, dreamerID, ""))
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rows, err := client.ListMemories(cleanup, url.Values{"kb_id": {kb.ID}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}})
		if err == nil {
			for _, memory := range rows.Items {
				_ = client.DeleteMemory(cleanup, memory.ID)
			}
		}
		_ = client.Do(cleanup, http.MethodDelete, "/api/v1/knowledge-bases/"+kb.ID, nil, nil)
	}()
	byID := map[string]Case{}
	for _, tc := range cases {
		saved, err := client.SaveMemory(ctx, kb.ID, tc.Content)
		require.NoError(t, err)
		if !saved.Created {
			// Recorded, never silently relabelled: a deduplicated fixture is not evaluated.
			report.Skipped = append(report.Skipped, tc.ID)
			continue
		}
		memory, err := client.PatchMemory(ctx, saved.Memory.ID, map[string]any{
			"verdict": tc.Verdict, "importance": tc.Importance, "tier": 1, "expected_updated_at": saved.Memory.UpdatedAt,
		})
		require.NoError(t, err)
		byID[memory.ID] = tc
		if tc.Positive {
			report.Positives++
		} else {
			report.Negatives++
		}
	}
	read := func() map[string]rest.Memory {
		rows, err := client.ListMemories(ctx, url.Values{"kb_id": {kb.ID}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}})
		require.NoError(t, err)
		out := map[string]rest.Memory{}
		for _, memory := range rows.Items {
			require.Equal(t, kb.ID, memory.KbID)
			out[memory.ID] = memory
		}
		return out
	}
	before := read()
	require.Len(t, before, len(byID))
	var response struct{ Data types.DreamResult }
	started := time.Now()
	for attempt := 0; attempt < 5; attempt++ {
		err = dreamClient(client).Do(ctx, "POST", "/api/v1/memories/dream", map[string]any{"kb_id": kb.ID}, &response)
		if err == nil || !strings.Contains(err.Error(), "dreamer: LLM call failed") {
			break
		}
		time.Sleep(10 * time.Second)
	}
	require.NoError(t, err)
	report.PassSeconds = append(report.PassSeconds, time.Since(started).Seconds())
	result := response.Data
	report.Proposed += result.ActionsProposed
	report.Applied += result.ActionsApplied
	if result.ActionsProposed == 0 {
		report.EmptyReplies++
	}
	after := read()
	if len(after) != len(before) {
		report.SafetyViolations++
	}
	for id, old := range before {
		now, ok := after[id]
		tc := byID[id]
		if !ok || now.Content != old.Content || now.TenantID != old.TenantID {
			report.SafetyViolations++
			continue
		}
		if old.Verdict == "fixed" || old.Verdict == "decision" {
			if now.Verdict != old.Verdict || now.Importance != old.Importance {
				report.SafetyViolations++
			}
		}
		if tc.Positive {
			switch tc.Kind {
			case "wip", "gotcha":
				if now.Verdict == tc.ExpectedVerdict {
					report.PositiveCorrect++
				}
			case "importance":
				if now.Importance == tc.Importance+tc.AllowedDelta {
					report.PositiveCorrect++
				}
			}
		}
	}
	for _, action := range result.Actions {
		tc, known := byID[action.TargetID]
		if !known {
			report.SafetyViolations++
			continue
		}
		correct := tc.Positive && ((action.Type == "update_verdict" && action.NewVerdict == tc.ExpectedVerdict) ||
			(action.Type == "adjust_importance" && tc.AllowedDelta != 0 && action.Delta == tc.AllowedDelta))
		if correct {
			report.CorrectActions++
		} else {
			report.IncorrectActions++
			t.Logf("incorrect action on %s: %s verdict=%s delta=%d reason=%q", tc.ID, action.Type, action.NewVerdict, action.Delta, action.Reason)
		}
	}
	t.Logf("kb %d: proposed=%d applied=%d in %.1fs", k, result.ActionsProposed, result.ActionsApplied, report.PassSeconds[len(report.PassSeconds)-1])
}
