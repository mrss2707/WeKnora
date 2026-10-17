package e2eclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Memory is a Memory V2 record as returned by the REST API.
type Memory struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	KbID        string    `json:"kb_id"`
	UserID      string    `json:"user_id"`
	Content     string    `json:"content"`
	MemoryType  string    `json:"memory_type"`
	Importance  int       `json:"importance"`
	Tier        int       `json:"tier"`
	Verdict     string    `json:"verdict"`
	HubScore    float64   `json:"hub_score"`
	AccessCount int64     `json:"access_count"`
	Tags        []string  `json:"tags"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedAt   time.Time `json:"created_at"`
	// Embedding must never be serialized to clients.
	Embedding any `json:"embedding,omitempty"`
}

// SaveResult is the POST /memories response.
type SaveResult struct {
	Memory     Memory `json:"memory"`
	Created    bool   `json:"created"`
	LintIssues []any  `json:"lint_issues"`
}

// SaveMemory stores a memory through the ingestion pipeline.
func (c *Client) SaveMemory(ctx context.Context, kbID, content string) (*SaveResult, error) {
	var resp struct{ Data SaveResult }
	err := c.Do(ctx, http.MethodPost, "/api/v1/memories", map[string]string{"kb_id": kbID, "content": content}, &resp)
	return &resp.Data, err
}

// ListResult is one server page of memories.
type ListResult struct {
	Items    []Memory `json:"items"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

// ListMemories lists memories with query parameters (kb_id is required).
// Rows come as search results ({memory, score}); they are unwrapped.
func (c *Client) ListMemories(ctx context.Context, q url.Values) (*ListResult, error) {
	var resp struct {
		Data struct {
			Items    []SearchHit `json:"items"`
			Total    int64       `json:"total"`
			Page     int         `json:"page"`
			PageSize int         `json:"page_size"`
		}
	}
	err := c.Do(ctx, http.MethodGet, "/api/v1/memories?"+q.Encode(), nil, &resp)
	out := &ListResult{Total: resp.Data.Total, Page: resp.Data.Page, PageSize: resp.Data.PageSize}
	for _, h := range resp.Data.Items {
		out.Items = append(out.Items, h.Memory)
	}
	return out, err
}

// SearchHit is one hybrid search result.
type SearchHit struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`
}

// SearchMemories runs the hybrid search pipeline.
func (c *Client) SearchMemories(ctx context.Context, kbID, query string, limit int) ([]SearchHit, error) {
	q := url.Values{"kb_id": {kbID}, "q": {query}, "limit": {fmt.Sprint(limit)}}
	var resp struct{ Data []SearchHit }
	err := c.Do(ctx, http.MethodGet, "/api/v1/memories/search?"+q.Encode(), nil, &resp)
	return resp.Data, err
}

// GetMemory reads one memory.
func (c *Client) GetMemory(ctx context.Context, id string) (*Memory, error) {
	var resp struct{ Data Memory }
	err := c.Do(ctx, http.MethodGet, "/api/v1/memories/"+id, nil, &resp)
	return &resp.Data, err
}

// PatchMemory applies a partial update (send expected_updated_at to lock).
func (c *Client) PatchMemory(ctx context.Context, id string, patch map[string]any) (*Memory, error) {
	var resp struct{ Data Memory }
	err := c.Do(ctx, http.MethodPut, "/api/v1/memories/"+id, patch, &resp)
	return &resp.Data, err
}

// DeleteMemory deletes a memory.
func (c *Client) DeleteMemory(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/memories/"+id, nil, nil)
}

// Stats is the GET /memories/stats response.
type Stats struct {
	TotalMemories int64            `json:"total_memories"`
	ByType        map[string]int64 `json:"by_type"`
	ByTier        map[string]int64 `json:"by_tier"`
	ByVerdict     map[string]int64 `json:"by_verdict"`
}

// GetStats reads a knowledge base's statistics.
func (c *Client) GetStats(ctx context.Context, kbID string) (*Stats, error) {
	var resp struct{ Data Stats }
	err := c.Do(ctx, http.MethodGet, "/api/v1/memories/stats?kb_id="+url.QueryEscape(kbID), nil, &resp)
	return &resp.Data, err
}

// GraphNode and GraphEdge mirror the frontend graph DTO.
type GraphNode struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	HubScore float64 `json:"hub_score"`
	Focal    bool    `json:"focal"`
}

type GraphEdge struct {
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
}

// Graph is a graph response.
type Graph struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Truncated bool        `json:"truncated"`
}

// GetGraphOverview reads a knowledge base's most connected memories.
func (c *Client) GetGraphOverview(ctx context.Context, kbID string) (*Graph, error) {
	var resp struct{ Data Graph }
	err := c.Do(ctx, http.MethodGet, "/api/v1/memories/graph?kb_id="+url.QueryEscape(kbID), nil, &resp)
	return &resp.Data, err
}

// GetGraph reads the neighbourhood of one memory.
func (c *Client) GetGraph(ctx context.Context, id string, depth int) (*Graph, error) {
	var resp struct{ Data Graph }
	err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/memories/graph/%s?depth=%d", id, depth), nil, &resp)
	return &resp.Data, err
}

// AuditEntry is one history row.
type AuditEntry struct {
	MemoryID string `json:"memory_id"`
	Actor    string `json:"actor"`
	UserID   string `json:"user_id"`
	Action   string `json:"action"`
}

// History reads the audit trail of a knowledge base.
func (c *Client) History(ctx context.Context, kbID string, pageSize int) ([]AuditEntry, int64, error) {
	var resp struct {
		Data struct {
			Items []AuditEntry `json:"items"`
			Total int64        `json:"total"`
		}
	}
	err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/memories/history?kb_id=%s&page_size=%d", url.QueryEscape(kbID), pageSize), nil, &resp)
	return resp.Data.Items, resp.Data.Total, err
}

// DreamResult is a dreamer pass outcome.
type DreamResult struct {
	ActionsProposed int `json:"actions_proposed"`
	ActionsApplied  int `json:"actions_applied"`
	Actions         []struct {
		Type     string `json:"type"`
		TargetID string `json:"target_id"`
	} `json:"actions"`
}

// Dream runs (or previews) a dreamer pass (Admin).
func (c *Client) Dream(ctx context.Context, kbID string, preview bool) (*DreamResult, error) {
	var resp struct{ Data DreamResult }
	err := c.Do(ctx, http.MethodPost, "/api/v1/memories/dream", map[string]any{"kb_id": kbID, "dry_run": preview}, &resp)
	return &resp.Data, err
}

// Relink rebuilds a knowledge base's relations (Admin).
func (c *Client) Relink(ctx context.Context, kbID string) error {
	return c.Do(ctx, http.MethodPost, "/api/v1/memories/relink", map[string]string{"kb_id": kbID}, nil)
}
