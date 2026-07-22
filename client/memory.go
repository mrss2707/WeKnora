// Package client provides typed SDK methods for the Memory V2 REST API.
// Endpoints mirrored from internal/handler/memory_v2.go.
package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// AgentMemory mirrors internal/types.AgentMemory. Kept in the client package
// to avoid a cross-module dependency from cli/ → internal/.
type AgentMemory struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id"`
	KbID           string   `json:"kb_id"`
	UserID         string   `json:"user_id"`
	Content        string   `json:"content"`
	MemoryType     string   `json:"memory_type"`
	Importance     int      `json:"importance"`
	Tier           int      `json:"tier"`
	Verdict        string   `json:"verdict"`
	HubScore       float64  `json:"hub_score"`
	AccessCount    int      `json:"access_count"`
	SessionID      string   `json:"session_id"`
	Tags           []string `json:"tags,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// MemorySearchResult wraps a memory with its relevance score.
type MemorySearchResult struct {
	Memory    *AgentMemory `json:"memory"`
	Score     float64      `json:"score"`
	StaleDays int          `json:"stale_days"`
	IsStale   bool         `json:"is_stale"`
}

// MemoryLintIssue describes a single lint finding.
type MemoryLintIssue struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	SourceID string `json:"source_id,omitempty"`
}

// SaveMemoryResult is the outcome of saving a memory.
type SaveMemoryResult struct {
	Memory     *AgentMemory      `json:"memory"`
	Created    bool              `json:"created"`
	LintIssues []MemoryLintIssue `json:"lint_issues"`
}

// MemoryHealthIssue describes a single health check finding.
type MemoryHealthIssue struct {
	Type        string `json:"type"`
	MemoryID    string `json:"memory_id"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Suggestion  string `json:"suggestion"`
}

// HealthReport is the overall health assessment.
type HealthReport struct {
	TenantID    string               `json:"tenant_id"`
	CheckedAt   string               `json:"checked_at"`
	TotalIssues int                  `json:"total_issues"`
	BySeverity  map[string]int       `json:"by_severity"`
	Issues      []*MemoryHealthIssue `json:"issues"`
}

// MemoryGraphNode is a node in a memory graph.
type MemoryGraphNode struct {
	ID       string  `json:"id"`
	Content  string  `json:"content"`
	Type     string  `json:"type"`
	Verdict  string  `json:"verdict"`
	HubScore float64 `json:"hub_score"`
}

// MemoryGraphEdge is an edge between two memory nodes.
type MemoryGraphEdge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Score  float64 `json:"score"`
}

// MemoryGraphResult holds nodes and edges for graph visualization.
type MemoryGraphResult struct {
	Nodes []MemoryGraphNode `json:"nodes"`
	Edges []MemoryGraphEdge `json:"edges"`
}

// MemoryStatusResult is the backend health + memory count response.
type MemoryStatusResult struct {
	Backend     string `json:"backend"`
	Available   bool   `json:"available"`
	MemoryCount int64  `json:"memory_count,omitempty"`
}

// CreateMemoryRequest is the payload for creating/updating a memory.
type CreateMemoryRequest struct {
	KbID       string   `json:"kb_id"`
	UserID     string   `json:"user_id,omitempty"`
	Content    string   `json:"content"`
	MemoryType string   `json:"memory_type,omitempty"`
	Importance int      `json:"importance,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	Tier       int      `json:"tier,omitempty"`
	Verdict    string   `json:"verdict,omitempty"`
}

// memoryListResponse is the API envelope for ListMemories.
type memoryListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items    []MemorySearchResult `json:"items"`
		Total    int64                `json:"total"`
		Page     int                  `json:"page"`
		PageSize int                  `json:"page_size"`
	} `json:"data"`
}

// memoryResponse is the API envelope for GetMemory.
type memoryResponse struct {
	Success bool         `json:"success"`
	Data    *AgentMemory `json:"data"`
}

// memorySaveResponse is the API envelope for CreateMemory / UpdateMemory.
type memorySaveResponse struct {
	Success bool              `json:"success"`
	Data    *SaveMemoryResult `json:"data"`
}

// memorySearchResponse is the API envelope for SearchMemories.
type memorySearchResponse struct {
	Success bool                  `json:"success"`
	Data    []MemorySearchResult  `json:"data"`
}

// memoryGraphResponse is the API envelope for GetMemoryGraph.
type memoryGraphResponse struct {
	Success bool             `json:"success"`
	Data    MemoryGraphResult `json:"data"`
}

// memoryStatsResponse is the API envelope for GetMemoryStats.
type memoryStatsResponse struct {
	Success bool `json:"success"`
	Data    struct {
		TotalMemories int64            `json:"total_memories"`
		ByType        map[string]int64 `json:"by_type"`
	} `json:"data"`
}

// memoryHealthResponse is the API envelope for GetHealthReport.
type memoryHealthResponse struct {
	Success bool         `json:"success"`
	Data    HealthReport `json:"data"`
}

// memoryStatusResponse is the API envelope for GetMemoryStatus.
type memoryStatusResponse struct {
	Success bool              `json:"success"`
	Data    MemoryStatusResult `json:"data"`
}

// ListMemories returns a paginated list of memories for a knowledge base.
func (c *Client) ListMemories(ctx context.Context, kbID string, page, pageSize int) ([]MemorySearchResult, int64, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(pageSize))
	if kbID != "" {
		q.Set("kb_id", kbID)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/memories", nil, q)
	if err != nil {
		return nil, 0, err
	}

	var envelope memoryListResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, 0, err
	}

	return envelope.Data.Items, envelope.Data.Total, nil
}

// GetMemory retrieves a single memory by ID.
func (c *Client) GetMemory(ctx context.Context, id string) (*AgentMemory, error) {
	path := fmt.Sprintf("/api/v1/memories/%s", id)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}

	var envelope memoryResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return envelope.Data, nil
}

// CreateMemory creates a new memory via the ingestion pipeline.
func (c *Client) CreateMemory(ctx context.Context, req *CreateMemoryRequest) (*SaveMemoryResult, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/memories", req, nil)
	if err != nil {
		return nil, err
	}

	var envelope memorySaveResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return envelope.Data, nil
}

// UpdateMemory updates an existing memory.
func (c *Client) UpdateMemory(ctx context.Context, id string, req *CreateMemoryRequest) (*SaveMemoryResult, error) {
	path := fmt.Sprintf("/api/v1/memories/%s", id)
	resp, err := c.doRequest(ctx, http.MethodPut, path, req, nil)
	if err != nil {
		return nil, err
	}

	var envelope memorySaveResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return envelope.Data, nil
}

// DeleteMemory soft-deletes a memory.
func (c *Client) DeleteMemory(ctx context.Context, id string) error {
	path := fmt.Sprintf("/api/v1/memories/%s", id)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil, nil)
	if err != nil {
		return err
	}

	return parseResponse(resp, nil)
}

// SearchMemories performs hybrid search across memories.
func (c *Client) SearchMemories(ctx context.Context, kbID, query string, limit int, memoryType, sessionID string, minScore float64) ([]MemorySearchResult, error) {
	q := url.Values{}
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if memoryType != "" {
		q.Set("memory_type", memoryType)
	}
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}
	if minScore > 0 {
		q.Set("min_score", strconv.FormatFloat(minScore, 'f', -1, 64))
	}
	if kbID != "" {
		q.Set("kb_id", kbID)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/memories/search", nil, q)
	if err != nil {
		return nil, err
	}

	var envelope memorySearchResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return envelope.Data, nil
}

// GetMemoryGraph returns a memory with its related nodes and edges.
func (c *Client) GetMemoryGraph(ctx context.Context, id, kbID string) (*MemoryGraphResult, error) {
	path := fmt.Sprintf("/api/v1/memories/graph/%s", id)
	q := url.Values{}
	if kbID != "" {
		q.Set("kb_id", kbID)
	}
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, q)
	if err != nil {
		return nil, err
	}

	var envelope memoryGraphResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return &envelope.Data, nil
}

// GetMemoryStats returns aggregate memory statistics.
func (c *Client) GetMemoryStats(ctx context.Context, kbID string) (map[string]int64, int64, error) {
	q := url.Values{}
	if kbID != "" {
		q.Set("kb_id", kbID)
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/memories/stats", nil, q)
	if err != nil {
		return nil, 0, err
	}

	var envelope memoryStatsResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, 0, err
	}

	return envelope.Data.ByType, envelope.Data.TotalMemories, nil
}

// GetHealthReport runs health checks and returns a HealthReport.
func (c *Client) GetHealthReport(ctx context.Context, kbID string) (*HealthReport, error) {
	q := url.Values{}
	if kbID != "" {
		q.Set("kb_id", kbID)
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/memories/health", nil, q)
	if err != nil {
		return nil, err
	}

	var envelope memoryHealthResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return &envelope.Data, nil
}

// GetMemoryStatus returns the memory backend status (backend, available, memory_count).
func (c *Client) GetMemoryStatus(ctx context.Context) (*MemoryStatusResult, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/tenants/memory-status", nil, nil)
	if err != nil {
		return nil, err
	}

	var envelope memoryStatusResponse
	if err := parseResponse(resp, &envelope); err != nil {
		return nil, err
	}

	return &envelope.Data, nil
}
