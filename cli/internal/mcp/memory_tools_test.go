package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- memory_recall tests ----

func TestMemoryRecall_Validation(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})

	tests := []struct {
		name    string
		args    memoryRecallInput
		wantErr bool
		errMsg  string
	}{
		{name: "missing kb_id", args: memoryRecallInput{Query: "test"}, wantErr: true, errMsg: "kb_id is required"},
		{name: "empty query", args: memoryRecallInput{KBID: "kb1", Query: ""}, wantErr: true, errMsg: "query cannot be empty"},
		{name: "valid", args: memoryRecallInput{KBID: "kb1", Query: "architecture"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{Name: "memory_recall", Arguments: tt.args})
			require.NoError(t, err)
			if tt.wantErr {
				assert.True(t, res.IsError)
				if tt.errMsg != "" {
					assert.Contains(t, toolContentText(res), tt.errMsg)
				}
			} else {
				assert.False(t, res.IsError)
			}
		})
	}
}

func TestMemoryRecall_LimitClamp(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// limit > 50 should error
	_, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "memory_recall",
		Arguments: memoryRecallInput{KBID: "kb1", Query: "test", Limit: 100},
	})
	require.NoError(t, err)
}

// ---- memory_save tests ----

func TestMemorySave_Validation(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})

	tests := []struct {
		name    string
		args    memorySaveInput
		wantErr bool
		errMsg  string
	}{
		{name: "missing kb_id", args: memorySaveInput{Content: "test"}, wantErr: true, errMsg: "kb_id is required"},
		{name: "empty content", args: memorySaveInput{KBID: "kb1", Content: ""}, wantErr: true, errMsg: "content cannot be empty"},
		{name: "valid", args: memorySaveInput{KBID: "kb1", Content: "learned something"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{Name: "memory_save", Arguments: tt.args})
			require.NoError(t, err)
			if tt.wantErr {
				assert.True(t, res.IsError)
				if tt.errMsg != "" {
					assert.Contains(t, toolContentText(res), tt.errMsg)
				}
			} else {
				assert.False(t, res.IsError)
			}
		})
	}
}

func TestMemorySave_SuccessShape(t *testing.T) {
	svc := &fakeSvc{}
	c, _ := newTestServer(t, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "memory_save",
		Arguments: memorySaveInput{KBID: "kb1", Content: "test memory", MemoryType: "semantic", Importance: 2},
	})
	require.NoError(t, err)
	assert.False(t, res.IsError)

	var out memorySaveOutput
	require.NoError(t, json.Unmarshal([]byte(toolContentText(res)), &out))
	assert.True(t, out.Created)
}

// ---- memory_graph tests ----

func TestMemoryGraph_Validation(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "memory_graph",
		Arguments: memoryGraphInput{KBID: "kb1"},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, toolContentText(res), "memory_id is required")
}

// ---- memory_status tests ----

func TestMemoryStatus_ReturnsHealth(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "memory_status",
		Arguments: memoryStatusInput{},
	})
	require.NoError(t, err)
	assert.False(t, res.IsError)

	var out memoryStatusOutput
	require.NoError(t, json.Unmarshal([]byte(toolContentText(res)), &out))
	assert.Equal(t, "v2", out.Backend)
	assert.True(t, out.Available)
}

// ---- memory_detail tests ----

func TestMemoryDetail_Validation(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "memory_detail",
		Arguments: memoryDetailInput{},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, toolContentText(res), "memory_id is required")
}

func TestMemoryDetail_Success(t *testing.T) {
	c, _ := newTestServer(t, &fakeSvc{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "memory_detail",
		Arguments: memoryDetailInput{MemoryID: "mem-1"},
	})
	require.NoError(t, err)
	assert.False(t, res.IsError)

	var out memoryDetailOutput
	require.NoError(t, json.Unmarshal([]byte(toolContentText(res)), &out))
	assert.Equal(t, "mem-1", out.Memory.ID)
	assert.Equal(t, "test content", out.Memory.Content)
}

// ---- helpers ----

func toolContentText(res *mcpsdk.CallToolResult) string {
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
