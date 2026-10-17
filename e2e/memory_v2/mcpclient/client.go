// Package mcpclient drives the WeKnora built-in MCP server (streamable HTTP)
// the way an agent does, for the Memory V2 end-to-end suite. The cmd/ tool
// in this directory wraps it for manual use.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Session is one initialized MCP connection authenticated with an endpoint
// token.
type Session struct {
	c *client.Client
}

// Connect opens and initializes a session to baseURL+path with a bearer token.
func Connect(ctx context.Context, baseURL, path, token string) (*Session, error) {
	cl, err := client.NewStreamableHttpClient(strings.TrimRight(baseURL, "/")+path,
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + token}))
	if err != nil {
		return nil, err
	}
	if err := cl.Start(ctx); err != nil {
		return nil, err
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "memv2-e2e", Version: "1.0"}
	if _, err := cl.Initialize(ctx, init); err != nil {
		_ = cl.Close()
		return nil, err
	}
	return &Session{c: cl}, nil
}

// Close ends the session.
func (s *Session) Close() error { return s.c.Close() }

// Tools lists the tool names the endpoint exposes.
func (s *Session) Tools(ctx context.Context) ([]string, error) {
	res, err := s.c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// ToolError is a tool call that returned isError=true.
type ToolError struct{ Message string }

func (e *ToolError) Error() string { return "tool error: " + e.Message }

// Call invokes a tool and decodes its structured (or JSON text) result.
func (s *Session) Call(ctx context.Context, name string, args map[string]any, out any) error {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := s.c.CallTool(ctx, req)
	if err != nil {
		return err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	text := sb.String()
	if res.IsError {
		return &ToolError{Message: text}
	}
	if out == nil {
		return nil
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("decode %s result: %w (%s)", name, err, text)
	}
	return nil
}

// Memory is the preview/detail shape returned by the memory tools.
type Memory struct {
	ID             string   `json:"id"`
	KBID           string   `json:"knowledge_base_id"`
	Content        string   `json:"content"`
	ContentPreview string   `json:"content_preview"`
	Verdict        string   `json:"verdict"`
	Score          float64  `json:"score"`
	Tags           []string `json:"tags"`
}

// RecallResult is memory_recall's output.
type RecallResult struct {
	Results []Memory `json:"results"`
	Total   int      `json:"total"`
}

// Recall runs memory_recall.
func (s *Session) Recall(ctx context.Context, kb, query string, limit int) (*RecallResult, error) {
	var out RecallResult
	err := s.Call(ctx, "memory_recall", map[string]any{"knowledge_base_id": kb, "query": query, "limit": limit}, &out)
	return &out, err
}

// SaveResult is memory_save's output.
type SaveResult struct {
	Memory  Memory `json:"memory"`
	Created bool   `json:"created"`
}

// Save runs memory_save.
func (s *Session) Save(ctx context.Context, kb, content string) (*SaveResult, error) {
	var out SaveResult
	err := s.Call(ctx, "memory_save", map[string]any{"knowledge_base_id": kb, "content": content}, &out)
	return &out, err
}

// Detail runs memory_detail.
func (s *Session) Detail(ctx context.Context, kb, id string) (*Memory, error) {
	var out struct {
		Memory Memory `json:"memory"`
	}
	err := s.Call(ctx, "memory_detail", map[string]any{"knowledge_base_id": kb, "memory_id": id}, &out)
	return &out.Memory, err
}

// GraphResult is memory_graph's output.
type GraphResult struct {
	MemoryID string   `json:"memory_id"`
	Nodes    []Memory `json:"nodes"`
	Edges    []struct {
		Source       string `json:"source"`
		Target       string `json:"target"`
		RelationType string `json:"relation_type"`
	} `json:"edges"`
}

// Graph runs memory_graph.
func (s *Session) Graph(ctx context.Context, kb, id string, depth int) (*GraphResult, error) {
	var out GraphResult
	err := s.Call(ctx, "memory_graph", map[string]any{"knowledge_base_id": kb, "memory_id": id, "depth": depth}, &out)
	return &out, err
}

// Status runs memory_status.
func (s *Session) Status(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := s.Call(ctx, "memory_status", map[string]any{}, &out)
	return out, err
}
