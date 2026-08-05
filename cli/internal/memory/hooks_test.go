package memory

import (
	"bytes"
	"context"
	"os"
	"testing"

	sdk "github.com/Tencent/WeKnora/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHookService struct{}

func (fakeHookService) SearchMemories(ctx context.Context, kbID, query string, limit int, memoryType, sessionID string, minScore float64) ([]sdk.MemorySearchResult, error) {
	return []sdk.MemorySearchResult{
		{
			Memory: &sdk.AgentMemory{
				ID:         "mem_1",
				Content:    "Remember to use WEKNORA_HOST for stateless hook authentication.",
				MemoryType: "decision",
				Importance: 2,
				Tags:       []string{"hooks"},
			},
			Score: 0.9,
		},
	}, nil
}

func (fakeHookService) CreateMemory(ctx context.Context, req *sdk.CreateMemoryRequest) (*sdk.SaveMemoryResult, error) {
	return &sdk.SaveMemoryResult{}, nil
}

func (fakeHookService) GetMemoryStatus(ctx context.Context) (*sdk.MemoryStatusResult, error) {
	return &sdk.MemoryStatusResult{Available: true, MemoryCount: 6}, nil
}

func (fakeHookService) ListKnowledgeBases(ctx context.Context) ([]sdk.KnowledgeBase, error) {
	return nil, nil
}

func TestRunSessionStartWritesPaiCodeHookSpecificOutput(t *testing.T) {
	t.Setenv("WEKNORA_KB_ID", "kb_abc")
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	})

	inR, inW, err := os.Pipe()
	require.NoError(t, err)
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = inR
	os.Stdout = outW

	_, err = inW.WriteString(`{"session_id":"s1","cwd":"/tmp"}` + "\n")
	require.NoError(t, err)
	require.NoError(t, inW.Close())

	err = RunHook("session-start", &HookContext{Client: fakeHookService{}, Cache: NewCache()})
	require.NoError(t, err)
	require.NoError(t, outW.Close())

	var buf bytes.Buffer
	_, err = buf.ReadFrom(outR)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"hookSpecificOutput"`)
	assert.Contains(t, buf.String(), `"hookEventName":"SessionStart"`)
	assert.Contains(t, buf.String(), `"additionalContext"`)
}
