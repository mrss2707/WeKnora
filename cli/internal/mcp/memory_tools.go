package mcp

import (
	"context"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	sdk "github.com/Tencent/WeKnora/client"
)

// ---- memory_recall ---------------------------------------------------------

type memoryRecallInput struct {
	KBID     string  `json:"kb_id" jsonschema:"knowledge base ID for the search scope"`
	Query    string  `json:"query" jsonschema:"natural-language query (2-4 keywords recommended)"`
	Limit    int     `json:"limit,omitempty" jsonschema:"max results (1..50); defaults to 10"`
	MinScore float64 `json:"min_score,omitempty" jsonschema:"minimum similarity threshold (0..1); defaults to 0.25"`
}

type memoryRecallOutput struct {
	Results []sdk.MemorySearchResult `json:"results"`
	Total   int                      `json:"total"`
	Query   string                   `json:"query"`
	Hint    string                   `json:"hint"`
}

func addMemoryRecall(server *mcpsdk.Server, svc memoryService) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "memory_recall",
		Description: "Hybrid search across saved agent memories. Returns compact results with memory id, content preview, type, importance, score, and tags. Use this at session start or when the user switches topics to load relevant past context. Call memory_detail(id) on interesting results.",
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           "Recall Memories",
			DestructiveHint: bptr(false),
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   bptr(false),
		},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in memoryRecallInput) (*mcpsdk.CallToolResult, any, error) {
		if in.KBID == "" {
			in.KBID = os.Getenv("WEKNORA_KB_ID")
		}
		if in.KBID == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeKBIDRequired, "kb_id is required; set WEKNORA_KB_ID env var or pass kb_id")), nil, nil
		}
		if strings.TrimSpace(in.Query) == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputMissingFlag, "query cannot be empty")), nil, nil
		}
		limit := in.Limit
		if limit < 1 {
			limit = 10
		}
		if limit > 50 {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "limit must be in 1..50")), nil, nil
		}
		minScore := in.MinScore
		if minScore <= 0 {
			minScore = 0.25
		}

		results, err := svc.SearchMemories(ctx, in.KBID, in.Query, limit, "", "", minScore)
		if err != nil {
			return toolErrorResult(cmdutil.WrapHTTP(err, "search memories")), nil, nil
		}
		if results == nil {
			results = []sdk.MemorySearchResult{}
		}
		return successResult(memoryRecallOutput{
			Results: results,
			Total:   len(results),
			Query:   in.Query,
			Hint:    "Use memory_detail(id) to load full content of a result.",
		}), nil, nil
	})
}

// ---- memory_save -----------------------------------------------------------

type memorySaveInput struct {
	KBID       string   `json:"kb_id" jsonschema:"knowledge base ID"`
	Content    string   `json:"content" jsonschema:"memory content (what to remember)"`
	MemoryType string   `json:"memory_type,omitempty" jsonschema:"semantic | episodic | procedural; defaults to semantic"`
	Importance int      `json:"importance,omitempty" jsonschema:"0 (low), 1 (normal), 2 (high), 3 (critical); defaults to 1"`
	Tags       string `json:"tags,omitempty" jsonschema:"concept-based tags (auth, api, database); max 8; no file names"`
	SessionID  string   `json:"session_id,omitempty" jsonschema:"session identifier for episodic grouping"`
}

type memorySaveOutput struct {
	Memory  *sdk.AgentMemory   `json:"memory"`
	Created bool               `json:"created"`
	Issues  []sdk.MemoryLintIssue `json:"lint_issues,omitempty"`
}

func addMemorySave(server *mcpsdk.Server, svc memoryService) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "memory_save",
		Description: "Save a memory through the full ingestion pipeline (embedding, dedup, lint, tier classification). Use after fixing a bug (memory_type=episodic, importance=high), making an architectural decision (memory_type=decision, importance=high), or learning something reusable (memory_type=semantic). Returns the saved memory with its id and any lint issues.",
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           "Save Memory",
			DestructiveHint: bptr(true),
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   bptr(false),
		},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in memorySaveInput) (*mcpsdk.CallToolResult, any, error) {
		if in.KBID == "" {
			in.KBID = os.Getenv("WEKNORA_KB_ID")
		}
		if in.KBID == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeKBIDRequired, "kb_id is required; set WEKNORA_KB_ID env var or pass kb_id")), nil, nil
		}
		if strings.TrimSpace(in.Content) == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputMissingFlag, "content cannot be empty")), nil, nil
		}
		mt := in.MemoryType
		if mt == "" {
			mt = "semantic"
		}
		imp := in.Importance
		if imp < 1 {
			imp = 1
		}
		if imp > 3 {
			imp = 3
		}
		if len(in.Tags) > 0 && len(strings.Split(in.Tags, ",")) > 8 {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "max 8 tags")), nil, nil
		}

		var tags []string
		if in.Tags != "" {
			for _, t := range strings.Split(in.Tags, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}

		result, err := svc.CreateMemory(ctx, &sdk.CreateMemoryRequest{
			KbID:       in.KBID,
			Content:    in.Content,
			MemoryType: mt,
			Importance: imp,
			Tags:       tags,
			SessionID:  in.SessionID,
		})
		if err != nil {
			return toolErrorResult(cmdutil.WrapHTTP(err, "create memory")), nil, nil
		}
		return successResult(memorySaveOutput{
			Memory:  result.Memory,
			Created: result.Created,
			Issues:  result.LintIssues,
		}), nil, nil
	})
}

// ---- memory_graph ----------------------------------------------------------

type memoryGraphInput struct {
	KBID     string `json:"kb_id" jsonschema:"knowledge base ID"`
	MemoryID string `json:"memory_id" jsonschema:"focal memory ID to center the graph on"`
}

type memoryGraphOutput struct {
	MemoryID string                 `json:"memory_id"`
	Nodes    []sdk.MemoryGraphNode  `json:"nodes"`
	Edges    []sdk.MemoryGraphEdge  `json:"edges"`
}

func addMemoryGraph(server *mcpsdk.Server, svc memoryService) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "memory_graph",
		Description: "Get a memory's relationship graph: the focal memory plus related memories (nodes) and their connections (edges). Use to visualize how memories cluster or find contradictions/duplicates before editing.",
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           "Memory Graph",
			DestructiveHint: bptr(false),
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   bptr(false),
		},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in memoryGraphInput) (*mcpsdk.CallToolResult, any, error) {
		if in.MemoryID == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputMissingFlag, "memory_id is required")), nil, nil
		}
		if in.KBID == "" {
			in.KBID = os.Getenv("WEKNORA_KB_ID")
		}

		graph, err := svc.GetMemoryGraph(ctx, in.MemoryID, in.KBID)
		if err != nil {
			return toolErrorResult(cmdutil.WrapHTTP(err, "get memory graph")), nil, nil
		}

		nodes := graph.Nodes
		if nodes == nil {
			nodes = []sdk.MemoryGraphNode{}
		}
		edges := graph.Edges
		if edges == nil {
			edges = []sdk.MemoryGraphEdge{}
		}
		return successResult(memoryGraphOutput{
			MemoryID: in.MemoryID,
			Nodes:    nodes,
			Edges:    edges,
		}), nil, nil
	})
}

// ---- memory_status ---------------------------------------------------------

type memoryStatusInput struct{}

type memoryStatusOutput struct {
	Backend     string `json:"backend"`
	Available   bool   `json:"available"`
	MemoryCount int64  `json:"memory_count"`
}

func addMemoryStatus(server *mcpsdk.Server, svc memoryService) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "memory_status",
		Description: "Check the memory backend health. Returns backend name, availability flag, and total memory count. Use at session start to verify the memory system is operational before calling memory_recall or memory_save.",
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           "Memory Status",
			DestructiveHint: bptr(false),
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   bptr(false),
		},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ memoryStatusInput) (*mcpsdk.CallToolResult, any, error) {
		result, err := svc.GetMemoryStatus(ctx)
		if err != nil {
			return toolErrorResult(cmdutil.WrapHTTP(err, "get memory status")), nil, nil
		}
		return successResult(memoryStatusOutput{
			Backend:     result.Backend,
			Available:   result.Available,
			MemoryCount: result.MemoryCount,
		}), nil, nil
	})
}

// ---- memory_detail ---------------------------------------------------------

type memoryDetailInput struct {
	MemoryID string `json:"memory_id" jsonschema:"memory ID to fetch full content for"`
}

type memoryDetailOutput struct {
	Memory *sdk.AgentMemory `json:"memory"`
}

func addMemoryDetail(server *mcpsdk.Server, svc memoryService) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "memory_detail",
		Description: "Fetch full content of a single memory by ID. Use after memory_recall returns compact previews to load the complete content of interesting results.",
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           "Memory Detail",
			DestructiveHint: bptr(false),
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   bptr(false),
		},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in memoryDetailInput) (*mcpsdk.CallToolResult, any, error) {
		if strings.TrimSpace(in.MemoryID) == "" {
			return toolErrorResult(cmdutil.NewError(cmdutil.CodeInputMissingFlag, "memory_id is required")), nil, nil
		}

		mem, err := svc.GetMemory(ctx, in.MemoryID)
		if err != nil {
			return toolErrorResult(cmdutil.WrapHTTP(err, "get memory")), nil, nil
		}
		return successResult(memoryDetailOutput{Memory: mem}), nil, nil
	})
}
