package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const memoryPreviewRunes = 500

type memoryV2ToolRegistrar struct {
	server *Server
	memory interfaces.ScopedMemoryV2Service
}

type memoryKBOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type memoryPreviewOutput struct {
	ID             string              `json:"id"`
	ContentPreview string              `json:"content_preview"`
	MemoryType     string              `json:"memory_type"`
	Importance     int                 `json:"importance"`
	Tier           int                 `json:"tier"`
	Verdict        types.MemoryVerdict `json:"verdict"`
	HubScore       float64             `json:"hub_score"`
	Score          float64             `json:"score,omitempty"`
	Tags           []string            `json:"tags"`
	CreatedAt      time.Time           `json:"created_at"`
	StaleDays      int                 `json:"stale_days,omitempty"`
	IsStale        bool                `json:"is_stale,omitempty"`
}

type memoryDetailOutput struct {
	ID         string              `json:"id"`
	KBID       string              `json:"knowledge_base_id"`
	Content    string              `json:"content"`
	MemoryType string              `json:"memory_type"`
	Importance int                 `json:"importance"`
	Tier       int                 `json:"tier"`
	Verdict    types.MemoryVerdict `json:"verdict"`
	HubScore   float64             `json:"hub_score"`
	Tags       []string            `json:"tags"`
	SessionID  string              `json:"session_id,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

type memoryRecallOutput struct {
	KnowledgeBase memoryKBOutput        `json:"knowledge_base"`
	Query         string                `json:"query"`
	Results       []memoryPreviewOutput `json:"results"`
	Total         int                   `json:"total"`
	Hint          string                `json:"hint"`
}

type memorySaveOutput struct {
	KnowledgeBase memoryKBOutput          `json:"knowledge_base"`
	Memory        memoryDetailOutput      `json:"memory"`
	Created       bool                    `json:"created"`
	LintIssues    []types.MemoryLintIssue `json:"lint_issues"`
}

type memoryDetailResult struct {
	KnowledgeBase memoryKBOutput     `json:"knowledge_base"`
	Memory        memoryDetailOutput `json:"memory"`
}

type memoryGraphEdgeOutput struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
}

type memoryGraphOutput struct {
	KnowledgeBase memoryKBOutput          `json:"knowledge_base"`
	MemoryID      string                  `json:"memory_id"`
	Nodes         []memoryPreviewOutput   `json:"nodes"`
	Edges         []memoryGraphEdgeOutput `json:"edges"`
	Truncated     bool                    `json:"truncated"`
}

// RegisterMemoryV2Tools installs the Memory V2 MCP module without expanding
// the base Server constructor or coupling the core catalog to optional runtime
// dependencies.
func RegisterMemoryV2Tools(s *Server, memory interfaces.ScopedMemoryV2Service) error {
	if s == nil || s.mcp == nil {
		return fmt.Errorf("memory V2 MCP registration requires a server")
	}
	if memory == nil {
		return fmt.Errorf("memory V2 MCP registration requires a scoped service")
	}
	r := &memoryV2ToolRegistrar{server: s, memory: memory}
	s.mcp.AddTools(
		server.ServerTool{Tool: memoryRecallTool(), Handler: r.handleRecall},
		server.ServerTool{Tool: memoryGraphTool(), Handler: r.handleGraph},
		server.ServerTool{Tool: memoryDetailTool(), Handler: r.handleDetail},
		server.ServerTool{Tool: memoryStatusTool(), Handler: r.handleStatus},
		server.ServerTool{Tool: memorySaveTool(), Handler: r.handleSave},
	)
	return nil
}

func memoryRecallTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolMemoryRecall,
		mcp.WithDescription("Hybrid search across Memory V2 records in one authorized knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Knowledge base id or exact name")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language memory query")),
		mcp.WithNumber("limit", mcp.Description("Maximum results, default 10, range 1..50"), mcp.Min(1), mcp.Max(50)),
		mcp.WithOutputSchema[memoryRecallOutput](),
		mcp.WithTitleAnnotation("Recall Memories"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func memorySaveTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolMemorySave,
		mcp.WithDescription("Save a memory through the Memory V2 embedding, deduplication, classification, and lint pipeline."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Knowledge base id or exact name")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Memory content, 10..10000 characters")),
		mcp.WithString("session_id", mcp.Description("Optional source session identifier")),
		mcp.WithOutputSchema[memorySaveOutput](),
		mcp.WithTitleAnnotation("Save Memory"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func memoryDetailTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolMemoryDetail,
		mcp.WithDescription("Fetch the full sanitized content of one Memory V2 record in an authorized knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Knowledge base id or exact name")),
		mcp.WithString("memory_id", mcp.Required(), mcp.Description("Memory id")),
		mcp.WithOutputSchema[memoryDetailResult](),
		mcp.WithTitleAnnotation("Memory Detail"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func memoryGraphTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolMemoryGraph,
		mcp.WithDescription("Return the persisted relation graph centered on one memory, bounded to one authorized knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Knowledge base id or exact name")),
		mcp.WithString("memory_id", mcp.Required(), mcp.Description("Focal memory id")),
		mcp.WithOutputSchema[memoryGraphOutput](),
		mcp.WithTitleAnnotation("Memory Graph"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func memoryStatusTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolMemoryStatus,
		mcp.WithDescription("Report Memory V2 readiness and the authenticated endpoint tenant's memory count."),
		mcp.WithOutputSchema[types.MemoryStatusResponse](),
		mcp.WithTitleAnnotation("Memory Status"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func (r *memoryV2ToolRegistrar) handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, kb, scoped, toolErr := r.authorizedKB(ctx, req, types.OrgRoleViewer)
	if toolErr != nil {
		return toolErr, nil
	}
	_ = ep
	query := strings.TrimSpace(req.GetString("query", ""))
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	limit := req.GetInt("limit", 10)
	if limit < 1 || limit > 50 {
		return mcp.NewToolResultError("limit must be in 1..50"), nil
	}
	results, err := r.memory.Recall(scoped, memoryScope(kb), query, limit)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to recall memories", err), nil
	}
	items := make([]memoryPreviewOutput, 0, len(results))
	for _, result := range results {
		items = append(items, previewMemory(result.Memory, result.Score, result.StaleDays, result.IsStale))
	}
	return jsonResult(memoryRecallOutput{
		KnowledgeBase: memoryKB(kb),
		Query:         query,
		Results:       items,
		Total:         len(items),
		Hint:          "Use memory_detail with this knowledge base and a memory id to load full content.",
	})
}

func (r *memoryV2ToolRegistrar) handleSave(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, kb, scoped, toolErr := r.authorizedKB(ctx, req, types.OrgRoleEditor)
	if toolErr != nil {
		return toolErr, nil
	}
	if err := access.RequireKBWrite(scoped, kb); err != nil {
		return mcp.NewToolResultError("this endpoint is not allowed to write to knowledge base " + kb.ID), nil
	}
	content := req.GetString("content", "")
	if strings.TrimSpace(content) == "" {
		return mcp.NewToolResultError("content is required"), nil
	}
	result, err := r.memory.Save(scoped, memoryScope(kb), content, req.GetString("session_id", ""))
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to save memory", err), nil
	}
	issues := result.LintIssues
	if issues == nil {
		issues = []types.MemoryLintIssue{}
	}
	return jsonResult(memorySaveOutput{
		KnowledgeBase: memoryKB(kb),
		Memory:        detailMemory(result.Memory),
		Created:       result.Created,
		LintIssues:    issues,
	})
}

func (r *memoryV2ToolRegistrar) handleDetail(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, kb, scoped, toolErr := r.authorizedKB(ctx, req, types.OrgRoleViewer)
	if toolErr != nil {
		return toolErr, nil
	}
	memoryID := strings.TrimSpace(req.GetString("memory_id", ""))
	if memoryID == "" {
		return mcp.NewToolResultError("memory_id is required"), nil
	}
	memory, err := r.memory.Detail(scoped, memoryScope(kb), memoryID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(memoryDetailResult{KnowledgeBase: memoryKB(kb), Memory: detailMemory(memory)})
}

func (r *memoryV2ToolRegistrar) handleGraph(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, kb, scoped, toolErr := r.authorizedKB(ctx, req, types.OrgRoleViewer)
	if toolErr != nil {
		return toolErr, nil
	}
	memoryID := strings.TrimSpace(req.GetString("memory_id", ""))
	if memoryID == "" {
		return mcp.NewToolResultError("memory_id is required"), nil
	}
	graph, err := r.memory.Graph(scoped, memoryScope(kb), memoryID, 100)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	nodes := make([]memoryPreviewOutput, 0, len(graph.Nodes)+1)
	nodes = append(nodes, previewMemory(graph.Focal, 0, 0, false))
	for _, memory := range graph.Nodes {
		nodes = append(nodes, previewMemory(memory, 0, 0, false))
	}
	edges := make([]memoryGraphEdgeOutput, 0, len(graph.Relations))
	for _, relation := range graph.Relations {
		edges = append(edges, memoryGraphEdgeOutput{
			ID: relation.ID, Source: relation.FromUUID, Target: relation.ToUUID,
			RelationType: relation.RelationType, Weight: relation.Weight,
		})
	}
	return jsonResult(memoryGraphOutput{
		KnowledgeBase: memoryKB(kb), MemoryID: graph.Focal.ID,
		Nodes: nodes, Edges: edges, Truncated: graph.Truncated,
	})
}

func (r *memoryV2ToolRegistrar) handleStatus(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	return jsonResult(r.memory.Status(ctx, strconv.FormatUint(ep.TenantID, 10)))
}

func (r *memoryV2ToolRegistrar) authorizedKB(
	ctx context.Context,
	req mcp.CallToolRequest,
	role types.OrgMemberRole,
) (*types.MCPEndpoint, *types.KnowledgeBase, context.Context, *mcp.CallToolResult) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return nil, nil, ctx, mcp.NewToolResultError("unauthorized")
	}
	selector := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	if selector == "" {
		return nil, nil, ctx, mcp.NewToolResultError("knowledge_base_id is required")
	}
	kbs, err := r.server.selectKnowledgeBases(ctx, ep, []string{selector})
	if err != nil {
		return nil, nil, ctx, mcp.NewToolResultError(err.Error())
	}
	kb := kbs[0]
	if kb.TenantID != ep.TenantID {
		return nil, nil, ctx, mcp.NewToolResultError("memory tools are not yet available for knowledge bases owned by another workspace")
	}
	scoped, err := r.server.scopedKBContext(ctx, kb, role)
	if err != nil {
		return nil, nil, ctx, mcp.NewToolResultError(err.Error())
	}
	return ep, kb, scoped, nil
}

func memoryScope(kb *types.KnowledgeBase) interfaces.MemoryV2Scope {
	return interfaces.MemoryV2Scope{
		TenantID:        strconv.FormatUint(kb.TenantID, 10),
		KnowledgeBaseID: kb.ID,
	}
}

func memoryKB(kb *types.KnowledgeBase) memoryKBOutput {
	return memoryKBOutput{ID: kb.ID, Name: kb.Name}
}

func previewMemory(memory *types.AgentMemory, score float64, staleDays int, stale bool) memoryPreviewOutput {
	if memory == nil {
		return memoryPreviewOutput{Tags: []string{}}
	}
	tags := append([]string(nil), memory.Tags...)
	if tags == nil {
		tags = []string{}
	}
	return memoryPreviewOutput{
		ID: memory.ID, ContentPreview: truncateRunes(memory.Content, memoryPreviewRunes),
		MemoryType: memory.MemoryType, Importance: memory.Importance, Tier: memory.Tier,
		Verdict: memory.Verdict, HubScore: memory.HubScore, Score: score, Tags: tags,
		CreatedAt: memory.CreatedAt, StaleDays: staleDays, IsStale: stale,
	}
}

func detailMemory(memory *types.AgentMemory) memoryDetailOutput {
	if memory == nil {
		return memoryDetailOutput{Tags: []string{}}
	}
	tags := append([]string(nil), memory.Tags...)
	if tags == nil {
		tags = []string{}
	}
	return memoryDetailOutput{
		ID: memory.ID, KBID: memory.KbID, Content: memory.Content,
		MemoryType: memory.MemoryType, Importance: memory.Importance, Tier: memory.Tier,
		Verdict: memory.Verdict, HubScore: memory.HubScore, Tags: tags,
		SessionID: memory.SessionID, CreatedAt: memory.CreatedAt, UpdatedAt: memory.UpdatedAt,
	}
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max]) + "…"
}
