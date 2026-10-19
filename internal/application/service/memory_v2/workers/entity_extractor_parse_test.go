package workers

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestEntityExtractor_ParsesFencedReply(t *testing.T) {
	reply := "Here are the entities:\n```json\n[{\"memory_index\":1,\"entities\":[{\"name\":\"Kubernetes\",\"type\":\"Technology\",\"confidence\":0.9}]}]\n```"
	mockChat := &mockEntityExtractorChat{chatFunc: func(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: reply}, nil
	}}
	repo := &mockEntityExtractorRepo{}
	e := newTestEntityExtractor(repo, mockChat)
	mem := makeExtractorMemory("mem-1", "tenant-1", "We run Kubernetes")

	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Contains(t, []string(mem.Tags), "Kubernetes", "fenced JSON from hosted models is still parsed")
}

func TestEntityJSON(t *testing.T) {
	assert.Equal(t, `[{"a":1}]`, entityJSON("```json\n[{\"a\":1}]\n```"))
	assert.Equal(t, "no json", entityJSON("no json"))
}
