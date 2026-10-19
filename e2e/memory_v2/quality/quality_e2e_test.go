//go:build memory_e2e

package quality

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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

type qualityCaseResult struct {
	ID, Language, Kind string
	Positive           bool
	Score              Score
	Result             types.DreamResult
	WarmupApplied      int
}

type qualityReport struct {
	Suite                                                               string
	Passed                                                              bool
	ServerDigest                                                        string
	ModelDigests                                                        map[string]string
	Model, RunID, CorpusDigest                                          string
	ExpectedPositives, ExpectedNegatives                                int
	EvaluatedPositives, EvaluatedNegatives                              int
	PositiveCorrect, CorrectActions, IncorrectActions, SafetyViolations int
	Precision, PositiveRecall                                           float64
	Qualified, FailFast                                                 bool
	Cases                                                               []qualityCaseResult
}

// TestDreamerQuality is deliberately separate from the short functional suite.
// It never substitutes a fabricated model response or changes production policy.
func TestDreamerQuality(t *testing.T) {
	if os.Getenv("MEMV2_E2E_QUALITY") != "1" {
		t.Skip("set MEMV2_E2E_QUALITY=1 for the real-model quality gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Minute)
	defer cancel()
	world, err := rest.LoadIsolatedWorld(ctx)
	require.NoError(t, err)
	model := os.Getenv("MEMV2_E2E_QUALITY_MODEL")
	if model == "" {
		model = "qwen2.5:3b"
	}
	modelURL := "http://localhost:11434/v1"
	if world.BaseURL == "http://localhost:18082" {
		modelURL = "http://host.docker.internal:11434/v1"
	}
	embedURL, chatURL, chatKey := modelURL, modelURL, "e2e"
	if remote := os.Getenv("MEMV2_E2E_QUALITY_REMOTE"); remote != "" {
		// Owner-authorized hosted candidate (synthetic corpus only). The key is read
		// from the owner's provider file at run time and is never logged or persisted
		// outside the isolated E2E database.
		chatURL, chatKey = remoteProvider(t, remote, model)
	} else {
		require.Contains(t, []string{"qwen2.5:3b", "llama3.2:3b"}, model, "only existing local models are authorized")
	}
	cases := Corpus()
	suite := "development"
	if os.Getenv("MEMV2_E2E_QUALITY_HOLDOUT") == "1" {
		cases, suite = HoldoutCorpus(), "holdout"
	}
	encoded, err := json.Marshal(cases)
	require.NoError(t, err)
	report := qualityReport{Model: model, RunID: uuid.NewString(), CorpusDigest: fmt.Sprintf("%x", sha256.Sum256(encoded)), ExpectedPositives: 100, ExpectedNegatives: 100}
	report.Suite = suite
	report.ServerDigest, report.ModelDigests = qualityProvenance(t, ctx, world.BaseURL, model)
	_, source, _, _ := runtime.Caller(0)
	out := filepath.Join(filepath.Dir(source), "..", ".run", "quality-"+report.RunID+".json")
	writeReport := func() {
		data, err := json.MarshalIndent(report, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(out, data, 0600))
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(out), 0700))
	t.Cleanup(writeReport)
	t.Logf("quality model=%s corpus=%s report=%s", model, report.CorpusDigest, out)
	var client *rest.Client
	var embedID, chatID, dreamerID string
	// Optional split configuration: the KB summary model handles text while the
	// per-KB setting points the Dreamer at another model of the same provider.
	dreamerModel := os.Getenv("MEMV2_E2E_QUALITY_DREAMER_MODEL")
	if dreamerModel != "" {
		require.NotEmpty(t, os.Getenv("MEMV2_E2E_QUALITY_REMOTE"), "a split Dreamer model needs a hosted provider")
		report.Model = model + " (dreamer=" + dreamerModel + ")"
	}
	for i, tc := range cases {
		if i%80 == 0 {
			name := "quality-" + report.RunID[:8] + "-" + strconv.Itoa(i/80)
			session, err := rest.Register(ctx, world.BaseURL, name, name+"@memv2-e2e.test", "Memv2-e2e!")
			require.NoError(t, err)
			client = session.As(world.BaseURL)
			embedID, err = client.CreateModel(ctx, rest.ModelSpec{Name: "bge-m3", Type: "Embedding", BaseURL: embedURL, APIKey: "e2e", Dimension: 1024})
			require.NoError(t, err)
			chatID, err = client.CreateModel(ctx, rest.ModelSpec{Name: model, Type: "KnowledgeQA", BaseURL: chatURL, APIKey: chatKey, Provider: os.Getenv("MEMV2_E2E_QUALITY_PROVIDER")})
			require.NoError(t, err)
			if dreamerModel != "" {
				dreamerURL, dreamerKey := remoteProvider(t, os.Getenv("MEMV2_E2E_QUALITY_REMOTE"), dreamerModel)
				dreamerID, err = client.CreateModel(ctx, rest.ModelSpec{Name: dreamerModel, Type: "KnowledgeQA", BaseURL: dreamerURL, APIKey: dreamerKey, Provider: os.Getenv("MEMV2_E2E_QUALITY_PROVIDER")})
				require.NoError(t, err)
			}
		}
		outcome := runQualityCase(t, ctx, client, embedID, chatID, dreamerID, report.RunID, tc)
		report.Cases = append(report.Cases, outcome)
		if tc.Positive {
			report.EvaluatedPositives++
			if outcome.Score.PositiveCorrect {
				report.PositiveCorrect++
			}
		} else {
			report.EvaluatedNegatives++
		}
		report.CorrectActions += outcome.Score.CorrectActions
		report.IncorrectActions += outcome.Score.IncorrectActions + outcome.WarmupApplied
		report.SafetyViolations += outcome.Score.SafetyViolations
		report.Precision = float64(report.CorrectActions) / float64(max(1, report.CorrectActions+report.IncorrectActions))
		report.PositiveRecall = float64(report.PositiveCorrect) / float64(max(1, report.EvaluatedPositives))
		writeReport()
		t.Logf("case=%s applied=%d correct=%t precision=%.3f positive_recall=%.3f", tc.ID, outcome.Result.ActionsApplied, outcome.Score.PositiveCorrect, report.Precision, report.PositiveRecall)
		if report.SafetyViolations > 0 || report.PositiveCorrect+report.ExpectedPositives-report.EvaluatedPositives < 80 {
			report.FailFast = true
			writeReport()
			t.Fatalf("quality gate cannot pass: positives=%d/%d, safety=%d; no thresholds or labels were relaxed", report.PositiveCorrect, report.EvaluatedPositives, report.SafetyViolations)
		}
	}
	require.Equal(t, 100, report.EvaluatedPositives)
	require.Equal(t, 100, report.EvaluatedNegatives)
	require.Zero(t, report.SafetyViolations)
	require.GreaterOrEqual(t, report.Precision, .95)
	require.GreaterOrEqual(t, report.PositiveRecall, .80)
	report.Passed = report.Precision >= .95 && report.PositiveRecall >= .80 && report.SafetyViolations == 0
	report.Qualified = report.Passed && report.Suite == "holdout"
	writeReport()
}

func runQualityCase(t *testing.T, ctx context.Context, client *rest.Client, embedID, chatID, dreamerID, runID string, tc Case) qualityCaseResult {
	t.Helper()
	kb, err := client.CreateKnowledgeBase(ctx, "memv2-quality-"+runID[:8]+"-"+tc.ID, embedID, chatID)
	require.NoError(t, err)
	if dreamerID != "" {
		require.NoError(t, client.SetMemoryModelSettings(ctx, kb.ID, dreamerID, ""))
	}
	// Cleanup is scoped to the just-created fixture KB, even after assertion failures.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		rows, err := client.ListMemories(cleanup, url.Values{"kb_id": {kb.ID}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}})
		if err != nil {
			t.Errorf("quality fixture cleanup list failed: %v", err)
			return
		}
		for _, memory := range rows.Items {
			if err := client.DeleteMemory(cleanup, memory.ID); err != nil {
				t.Errorf("quality fixture memory cleanup: %v", err)
			}
		}
		if err := client.Do(cleanup, http.MethodDelete, "/api/v1/knowledge-bases/"+kb.ID, nil, nil); err != nil {
			t.Errorf("quality fixture KB cleanup: %v", err)
		}
	}()
	prepare := func(content string) *rest.Memory {
		saved, err := client.SaveMemory(ctx, kb.ID, content)
		require.NoError(t, err)
		require.True(t, saved.Created, "evaluation must not reuse a preexisting memory")
		memory, err := client.PatchMemory(ctx, saved.Memory.ID, map[string]any{
			"verdict": tc.Verdict, "importance": tc.Importance, "tier": 1, "expected_updated_at": saved.Memory.UpdatedAt,
		})
		require.NoError(t, err)
		return memory
	}
	first := prepare(tc.Content)
	if tc.Kind == "merge" {
		second := prepare("Fixture staging: an amber telescope measures the orbital position of a distant moon using a brass reference dial.")
		_, err := client.PatchMemory(ctx, second.ID, map[string]any{"content": tc.MergeContent, "expected_updated_at": second.UpdatedAt})
		require.NoError(t, err)
		var graph struct {
			Data struct {
				Edges []struct {
					Weight float64 `json:"weight"`
				} `json:"edges"`
			}
		}
		require.NoError(t, client.Do(ctx, "GET", "/api/v1/memories/graph?kb_id="+kb.ID+"&limit=5", nil, &graph))
		eligible := false
		for _, edge := range graph.Data.Edges {
			eligible = eligible || edge.Weight >= .93
		}
		require.True(t, eligible, "invalid merge fixture: real stored cosine must meet the unchanged .93 policy before model evaluation")
	}
	var status struct{ Data types.DreamStatus }
	require.NoError(t, client.Do(ctx, "GET", "/api/v1/memories/dream?kb_id="+kb.ID, nil, &status))
	require.True(t, status.Data.Enabled)
	require.True(t, status.Data.Scheduled)
	require.False(t, status.Data.DryRun)
	read := func() []rest.Memory {
		rows, err := client.ListMemories(ctx, url.Values{"kb_id": {kb.ID}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}})
		require.NoError(t, err)
		for _, memory := range rows.Items {
			require.Equal(t, kb.ID, memory.KbID)
			require.Equal(t, first.TenantID, memory.TenantID)
			require.Nil(t, memory.Embedding)
		}
		return rows.Items
	}
	dream := func() types.DreamResult {
		var response struct{ Data types.DreamResult }
		// A hosted provider can reject a call (overload) before the Dreamer
		// has read or changed anything; only that pre-mutation failure is retried.
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			err = dreamClient(client).Do(ctx, "POST", "/api/v1/memories/dream", map[string]any{"kb_id": kb.ID}, &response)
			if err == nil || !strings.Contains(err.Error(), "dreamer: LLM call failed") {
				break
			}
			time.Sleep(10 * time.Second)
		}
		require.NoError(t, err)
		return response.Data
	}
	out := qualityCaseResult{ID: tc.ID, Language: tc.Language, Kind: tc.Kind, Positive: tc.Positive}
	if tc.Kind == "merge" {
		for pass := 0; pass < 3; pass++ {
			warm := dream()
			out.WarmupApplied += warm.ActionsApplied
			require.Len(t, read(), 2, "probation must not apply a merge")
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(6 * time.Second):
			}
		}
	}
	before := read()
	out.Result = dream()
	after := read()
	out.Score = Grade(tc, before, after, out.Result)
	history, _, err := client.History(ctx, kb.ID, 100)
	require.NoError(t, err)
	audited := 0
	for _, entry := range history {
		if entry.Actor == "dreamer" {
			audited++
		}
	}
	if out.Result.ActionsApplied+out.WarmupApplied > 0 {
		require.GreaterOrEqual(t, audited, out.Result.ActionsApplied+out.WarmupApplied, "every applied action must have a real audit")
	}
	return out
}

func qualityProvenance(t *testing.T, ctx context.Context, base, model string) (string, map[string]string) {
	t.Helper()
	var serverDigest string
	if base == "http://localhost:18082" {
		output, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Image}}", "memv2-release-gate-app-1").Output()
		require.NoError(t, err)
		serverDigest = strings.TrimSpace(string(output))
	} else {
		_, source, _, _ := runtime.Caller(0)
		binary, err := os.Open(filepath.Join(filepath.Dir(source), "..", ".run", "weknora-server"))
		require.NoError(t, err)
		defer binary.Close()
		hash := sha256.New()
		_, err = io.Copy(hash, binary)
		require.NoError(t, err)
		serverDigest = fmt.Sprintf("sha256:%x", hash.Sum(nil))
	}
	request, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:11434/api/tags", nil)
	require.NoError(t, err)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var tags struct {
		Models []struct {
			Name, Digest string
			Size         int64
		}
	}
	require.NoError(t, json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&tags))
	digests := map[string]string{}
	for _, wanted := range []string{model, "bge-m3"} {
		if wanted == model && os.Getenv("MEMV2_E2E_QUALITY_REMOTE") != "" {
			digests[wanted] = "remote:" + os.Getenv("MEMV2_E2E_QUALITY_REMOTE")
			continue
		}
		for _, entry := range tags.Models {
			if entry.Name == wanted || entry.Name == wanted+":latest" {
				require.Positive(t, entry.Size, "cloud models are not authorized")
				require.NotEmpty(t, entry.Digest)
				digests[wanted] = entry.Digest
			}
		}
		require.NotEmpty(t, digests[wanted], "required local model must already exist")
	}
	return serverDigest, digests
}

// remoteProvider resolves an owner-listed hosted provider and confirms the model
// belongs to it. Only the base URL and key are returned; nothing is logged.
func remoteProvider(t *testing.T, id, model string) (string, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(home, ".paicode", "providers.json"))
	require.NoError(t, err)
	var file struct {
		Provider map[string]struct {
			Options struct{ BaseURL, APIKey string }
			Models  map[string]json.RawMessage
		}
	}
	require.NoError(t, json.Unmarshal(data, &file))
	entry, ok := file.Provider[id]
	require.True(t, ok, "unknown provider id")
	_, ok = entry.Models[model]
	require.True(t, ok, "model is not listed for the provider")
	require.NotEmpty(t, entry.Options.APIKey)
	return strings.TrimRight(entry.Options.BaseURL, "/"), entry.Options.APIKey
}
