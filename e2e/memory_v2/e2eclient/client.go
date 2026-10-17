// Package e2eclient is a small typed client for the public WeKnora REST API,
// used by the Memory V2 end-to-end seed tool and tests. It only calls public
// endpoints, exactly like the web UI does.
package e2eclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the API as one signed-in user.
type Client struct {
	BaseURL string // e.g. http://localhost:18080
	Token   string
	HTTP    *http.Client
}

// New returns an anonymous client.
func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 120 * time.Second}}
}

// APIError is a non-2xx response.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// StatusOf returns the HTTP status of an APIError (0 otherwise).
func StatusOf(err error) int {
	if e, ok := err.(*APIError); ok {
		return e.Status
	}
	return 0
}

// Do sends a JSON request and decodes the JSON response into out (if non-nil).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s %s: %w (%s)", method, path, err, truncate(string(raw), 300))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------------------
// Auth and tenants
// ---------------------------------------------------------------------------

// Membership is one workspace a user can act in.
type Membership struct {
	TenantID uint64 `json:"tenant_id"`
	Role     string `json:"role"`
}

// Session is a signed-in user.
type Session struct {
	UserID      string
	Email       string
	Token       string
	HomeTenant  uint64
	Memberships []Membership
}

// Register creates an account (ignored when it already exists) and signs in.
func Register(ctx context.Context, baseURL, username, email, password string) (*Session, error) {
	c := New(baseURL)
	err := c.Do(ctx, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
	}, nil)
	if err != nil && StatusOf(err) != http.StatusConflict && StatusOf(err) != http.StatusBadRequest {
		return nil, err
	}
	return Login(ctx, baseURL, email, password)
}

// Login signs in.
func Login(ctx context.Context, baseURL, email, password string) (*Session, error) {
	c := New(baseURL)
	var resp struct {
		Success      bool                `json:"success"`
		Message      string              `json:"message"`
		Token        string              `json:"token"`
		User         struct{ ID string } `json:"user"`
		ActiveTenant struct{ ID uint64 } `json:"active_tenant"`
		Memberships  []Membership        `json:"memberships"`
	}
	if err := c.Do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": password}, &resp); err != nil {
		return nil, err
	}
	if resp.Token == "" {
		return nil, fmt.Errorf("login %s: %s", email, resp.Message)
	}
	return &Session{UserID: resp.User.ID, Email: email, Token: resp.Token, HomeTenant: resp.ActiveTenant.ID, Memberships: resp.Memberships}, nil
}

// As returns a client acting as the session in its active workspace.
func (s *Session) As(baseURL string) *Client {
	c := New(baseURL)
	c.Token = s.Token
	return c
}

// SwitchTenant re-issues the session token for another workspace.
func (s *Session) SwitchTenant(ctx context.Context, baseURL string, tenantID uint64) (*Session, error) {
	var resp struct {
		Token string `json:"token"`
	}
	if err := s.As(baseURL).Do(ctx, http.MethodPost, "/api/v1/auth/switch-tenant", map[string]any{"tenant_id": tenantID}, &resp); err != nil {
		return nil, err
	}
	if resp.Token == "" {
		return nil, fmt.Errorf("switch-tenant returned no token")
	}
	out := *s
	out.Token = resp.Token
	return &out, nil
}

// AddTenantMember adds a registered user to a workspace (Owner only).
func (c *Client) AddTenantMember(ctx context.Context, tenantID uint64, email, role string) error {
	err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/tenants/%d/members", tenantID), map[string]string{"email": email, "role": role}, nil)
	if StatusOf(err) == http.StatusConflict {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Organizations and sharing
// ---------------------------------------------------------------------------

// Organization is a shared space.
type Organization struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	InviteCode string `json:"invite_code"`
}

// CreateOrganization creates a shared space owned by the caller's workspace.
func (c *Client) CreateOrganization(ctx context.Context, name string) (*Organization, error) {
	var resp struct{ Data Organization }
	if err := c.Do(ctx, http.MethodPost, "/api/v1/organizations", map[string]string{"name": name}, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// JoinOrganization joins a shared space by invite code.
func (c *Client) JoinOrganization(ctx context.Context, code string) error {
	err := c.Do(ctx, http.MethodPost, "/api/v1/organizations/join", map[string]string{"invite_code": code}, nil)
	if StatusOf(err) == http.StatusConflict {
		return nil
	}
	return err
}

// ShareKnowledgeBase shares a knowledge base with a space at a permission.
func (c *Client) ShareKnowledgeBase(ctx context.Context, kbID, orgID, permission string) error {
	err := c.Do(ctx, http.MethodPost, "/api/v1/knowledge-bases/"+kbID+"/shares",
		map[string]string{"organization_id": orgID, "permission": permission}, nil)
	if StatusOf(err) == http.StatusConflict {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Models and knowledge bases
// ---------------------------------------------------------------------------

// ModelSpec describes an OpenAI-compatible model endpoint.
type ModelSpec struct {
	Name, Type, BaseURL, APIKey string
	Dimension                   int
}

// CreateModel registers a remote model and returns its id.
func (c *Client) CreateModel(ctx context.Context, m ModelSpec) (string, error) {
	params := map[string]any{"base_url": m.BaseURL, "api_key": m.APIKey, "provider": "generic"}
	if m.Dimension > 0 {
		params["embedding_parameters"] = map[string]any{"dimension": m.Dimension}
	}
	var resp struct{ Data struct{ ID string } }
	err := c.Do(ctx, http.MethodPost, "/api/v1/models", map[string]any{
		"name": m.Name, "display_name": m.Name, "type": m.Type, "source": "remote", "parameters": params,
	}, &resp)
	return resp.Data.ID, err
}

// KnowledgeBase is the subset of a knowledge base the tests use.
type KnowledgeBase struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID uint64 `json:"tenant_id"`
}

// CreateKnowledgeBase creates a document knowledge base bound to models.
func (c *Client) CreateKnowledgeBase(ctx context.Context, name, embeddingModelID, chatModelID string) (*KnowledgeBase, error) {
	var resp struct{ Data KnowledgeBase }
	err := c.Do(ctx, http.MethodPost, "/api/v1/knowledge-bases", map[string]any{
		"name": name, "type": "document", "embedding_model_id": embeddingModelID, "summary_model_id": chatModelID,
	}, &resp)
	return &resp.Data, err
}

// ListKnowledgeBases lists the knowledge bases of the active workspace.
func (c *Client) ListKnowledgeBases(ctx context.Context) ([]KnowledgeBase, error) {
	var resp struct{ Data []KnowledgeBase }
	err := c.Do(ctx, http.MethodGet, "/api/v1/knowledge-bases", nil, &resp)
	return resp.Data, err
}

// ---------------------------------------------------------------------------
// MCP endpoints
// ---------------------------------------------------------------------------

// MCPEndpoint is a created endpoint with its one-time token.
type MCPEndpoint struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Token string `json:"token"`
}

// CreateMCPEndpoint creates an MCP endpoint (Admin).
func (c *Client) CreateMCPEndpoint(ctx context.Context, name string, kbIDs, tools []string, ratePerMinute int) (*MCPEndpoint, error) {
	body := map[string]any{"name": name, "knowledge_base_ids": kbIDs, "tools": tools}
	if ratePerMinute > 0 {
		body["rate_limit_per_minute"] = ratePerMinute
	}
	var resp struct{ Data MCPEndpoint }
	err := c.Do(ctx, http.MethodPost, "/api/v1/mcp-endpoints", body, &resp)
	return &resp.Data, err
}

// SetMCPEndpointEnabled enables or disables an endpoint.
func (c *Client) SetMCPEndpointEnabled(ctx context.Context, id string, enabled bool) error {
	return c.Do(ctx, http.MethodPut, "/api/v1/mcp-endpoints/"+id, map[string]any{"enabled": enabled}, nil)
}

// RotateMCPEndpointToken rotates an endpoint token and returns the new one.
func (c *Client) RotateMCPEndpointToken(ctx context.Context, id string) (string, error) {
	var resp struct{ Data MCPEndpoint }
	err := c.Do(ctx, http.MethodPost, "/api/v1/mcp-endpoints/"+id+"/rotate-token", nil, &resp)
	return resp.Data.Token, err
}
