//go:build memory_e2e

package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// TestDreamerModelProbe diagnoses raw model/schema behavior only. It does not
// qualify a model or substitute for the public-API action/audit quality gate.
func TestDreamerModelProbe(t *testing.T) {
	if os.Getenv("MEMV2_E2E_MODEL_PROBE") != "1" {
		t.Skip("opt-in local model diagnostic")
	}
	model := os.Getenv("MEMV2_E2E_QUALITY_MODEL")
	if model == "" {
		model = "qwen2.5:3b"
	}
	require.Contains(t, []string{"qwen2.5:3b", "llama3.2:3b"}, model)
	worker := NewDreamerWorker(nil, nil, types.DefaultMemoryV2Config().Dreamer, nil)
	for _, content := range []string{
		"The snapshot exporter implementation remains unfinished. Development is still in progress and review has not been completed.",
		"Việc triển khai snapshot exporter vẫn đang dang dở. Nhóm đang tiếp tục phát triển, chưa hoàn thành bước kiểm duyệt.",
	} {
		memory := &types.AgentMemory{Content: content, Verdict: types.VerdictNone, Importance: 3, CreatedAt: time.Now()}
		format := dreamerResponseFormat(1, worker.config.MaxActions, false)
		prompt := worker.buildPrompt([]*types.AgentMemory{memory}) + "\nUse this JSON schema: " + string(format)
		body, err := json.Marshal(map[string]any{
			"model": model, "temperature": 0, "max_tokens": 2000, "stream": false,
			"response_format": map[string]string{"type": "json_object"},
			"messages":        []map[string]string{{"role": "system", "content": dreamerSystemPrompt}, {"role": "user", "content": prompt}},
		})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		request, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:11434/v1/chat/completions", bytes.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		var result struct {
			Choices []struct{ Message struct{ Content string } }
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result)
		response.Body.Close()
		cancel()
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.NoError(t, err)
		require.NotEmpty(t, result.Choices)
		raw := result.Choices[0].Message.Content
		t.Logf("diagnostic only, model=%s raw=%q parsed=%+v", model, raw, parseDreamActions(raw))
	}
}
