package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
)

type recordingScopedMemoryV2 struct {
	stubScopedMemoryV2
	lastScope    interfaces.MemoryV2Scope
	lastQuery    string
	lastLimit    int
	savedContent string
}

func (f *recordingScopedMemoryV2) Recall(_ context.Context, scope interfaces.MemoryV2Scope, query string, limit int) ([]*types.MemorySearchResult, error) {
	f.lastScope, f.lastQuery, f.lastLimit = scope, query, limit
	return []*types.MemorySearchResult{{
		Memory: &types.AgentMemory{
			ID: "mem-1", TenantID: scope.TenantID, KbID: scope.KnowledgeBaseID,
			Content: "stored secret without internal metadata", MemoryType: "semantic",
			CreatedAt: time.Unix(1, 0), Metadata: []byte(`{"private":true}`),
		},
		Score: 0.8,
	}}, nil
}

func (f *recordingScopedMemoryV2) Save(_ context.Context, scope interfaces.MemoryV2Scope, content, _ string) (*types.SaveMemoryResult, error) {
	f.lastScope, f.savedContent = scope, content
	return &types.SaveMemoryResult{Created: true, Memory: &types.AgentMemory{
		ID: "mem-1", TenantID: scope.TenantID, KbID: scope.KnowledgeBaseID,
		Content: content, MemoryType: "semantic", CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}}, nil
}

func newMemoryToolTestEngine(t *testing.T, ep *types.MCPEndpoint, memory interfaces.ScopedMemoryV2Service, kbs ...*types.KnowledgeBase) *gin.Engine {
	t.Helper()
	kbService := &stubKBService{kbs: map[string]*types.KnowledgeBase{}}
	for _, kb := range kbs {
		kbService.kbs[kb.ID] = kb
	}
	srv := NewServer(kbService, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := RegisterMemoryV2Tools(srv, memory); err != nil {
		t.Fatalf("register Memory V2 tools: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/mcp/:endpoint_id", func(c *gin.Context) {
		c.Request = c.Request.WithContext(mcpCallContext(ep.TenantID, ep))
		c.Next()
	}, gin.WrapH(srv.Handler()))
	return r
}

func toolCallText(t *testing.T, r *gin.Engine, name string, arguments map[string]any) (string, bool) {
	t.Helper()
	resp := rpc(t, r, "tools/call", map[string]any{"name": name, "arguments": arguments})
	result, _ := resp["result"].(map[string]any)
	isError, _ := result["isError"].(bool)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return "", isError
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text, isError
}

func TestMemoryToolDefinitionsMatchSafetyContract(t *testing.T) {
	readTools := []mcp.Tool{memoryRecallTool(), memoryGraphTool(), memoryDetailTool(), memoryStatusTool()}
	for _, tool := range readTools {
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s must be read-only", tool.Name)
		}
		if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
			t.Fatalf("%s must be idempotent", tool.Name)
		}
		if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Fatalf("%s must be closed-world", tool.Name)
		}
	}

	recall := memoryRecallTool()
	if recall.Name != types.MCPEndpointToolMemoryRecall || recall.Annotations.ReadOnlyHint == nil || !*recall.Annotations.ReadOnlyHint {
		t.Fatal("memory_recall must be read-only")
	}
	if _, ok := recall.InputSchema.Properties["min_score"]; ok {
		t.Fatal("memory_recall must not expose ignored min_score")
	}
	save := memorySaveTool()
	if save.Annotations.DestructiveHint == nil || !*save.Annotations.DestructiveHint {
		t.Fatal("memory_save must advertise a mutation")
	}
	for _, misleading := range []string{"tenant_id", "user_id", "memory_type", "importance", "tags"} {
		if _, ok := save.InputSchema.Properties[misleading]; ok {
			t.Fatalf("memory_save must not expose %s", misleading)
		}
	}
}

func TestMemoryRecallUsesAuthorizedKnowledgeBaseScope(t *testing.T) {
	memory := &recordingScopedMemoryV2{}
	ep := &types.MCPEndpoint{
		ID: "ep-1", TenantID: 1, RateLimitPerMinute: 100,
		Tools:            types.StringArray{types.MCPEndpointToolMemoryRecall},
		KnowledgeBaseIDs: types.StringArray{"kb-1"},
	}
	r := newMemoryToolTestEngine(t, ep, memory, &types.KnowledgeBase{ID: "kb-1", Name: "Product", TenantID: 1})

	text, isError := toolCallText(t, r, types.MCPEndpointToolMemoryRecall, map[string]any{
		"knowledge_base_id": "product", "query": "architecture", "limit": 5,
	})
	if isError {
		t.Fatalf("memory_recall failed: %s", text)
	}
	if memory.lastScope.TenantID != "1" || memory.lastScope.KnowledgeBaseID != "kb-1" || memory.lastLimit != 5 {
		t.Fatalf("unexpected trusted scope: %+v limit=%d", memory.lastScope, memory.lastLimit)
	}
	if strings.Contains(text, "tenant_id") || strings.Contains(text, "\"metadata\"") || strings.Contains(text, "private") {
		t.Fatalf("response leaked internal fields: %s", text)
	}
}

func TestMemorySaveUsesAuthorizedKnowledgeBaseScope(t *testing.T) {
	memory := &recordingScopedMemoryV2{}
	kb := &types.KnowledgeBase{ID: "kb-1", Name: "Product", TenantID: 1}
	ep := &types.MCPEndpoint{
		ID: "ep-write", TenantID: 1, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolMemorySave}, KnowledgeBaseIDs: types.StringArray{"kb-1"},
	}
	r := newMemoryToolTestEngine(t, ep, memory, kb)
	text, isError := toolCallText(t, r, types.MCPEndpointToolMemorySave, map[string]any{
		"knowledge_base_id": "kb-1", "content": "valid memory content",
	})
	if isError {
		t.Fatalf("memory_save failed: %s", text)
	}
	if memory.savedContent != "valid memory content" || memory.lastScope.TenantID != "1" || memory.lastScope.KnowledgeBaseID != "kb-1" {
		t.Fatalf("unexpected save scope/content: scope=%+v content=%q", memory.lastScope, memory.savedContent)
	}
}

func TestMemoryToolsRejectForeignOwnerSharedKnowledgeBase(t *testing.T) {
	memory := &recordingScopedMemoryV2{}
	shared := &types.KnowledgeBase{ID: "kb-shared", Name: "Shared", TenantID: 2}
	ep := &types.MCPEndpoint{
		ID: "ep-1", TenantID: 1, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolMemoryRecall}, KnowledgeBaseIDs: types.StringArray{"kb-shared"},
	}
	kbService := &stubKBService{kbs: map[string]*types.KnowledgeBase{"kb-shared": shared}}
	srv := NewServer(kbService, nil, nil, nil, nil, nil, nil,
		&stubKBShareService{shared: map[string]types.OrgMemberRole{"kb-shared": types.OrgRoleViewer}},
		&stubTenantService{tenants: map[uint64]*types.Tenant{2: {ID: 2}}}, nil, nil, nil, nil)
	if err := RegisterMemoryV2Tools(srv, memory); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/mcp/:endpoint_id", func(c *gin.Context) {
		c.Request = c.Request.WithContext(mcpCallContext(1, ep))
		c.Next()
	}, gin.WrapH(srv.Handler()))

	text, isError := toolCallText(t, r, types.MCPEndpointToolMemoryRecall, map[string]any{
		"knowledge_base_id": "kb-shared", "query": "test",
	})
	if !isError || !strings.Contains(text, "owned by another workspace") {
		t.Fatalf("shared-owner memory must be rejected: error=%v text=%s", isError, text)
	}
}
