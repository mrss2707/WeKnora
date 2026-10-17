//go:build memory_e2e

package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// world is seed-output.json (written by e2e/memory_v2/seed).
type world struct {
	BaseURL        string `json:"base_url"`
	Password       string `json:"password"`
	Organization   string `json:"organization_id"`
	Canary         string `json:"canary"`
	KnowledgeBases map[string]struct {
		ID       string `json:"id"`
		TenantID uint64 `json:"tenant_id"`
		Share    string `json:"share"`
	} `json:"knowledge_bases"`
	Memories map[string]map[string]string `json:"memories"`
	MCP      map[string]struct {
		ID    string   `json:"id"`
		Path  string   `json:"path"`
		Token string   `json:"token"`
		KBs   []string `json:"knowledge_bases"`
		Tools []string `json:"tools"`
	} `json:"mcp_endpoints"`
	Users map[string]struct {
		Email  string `json:"email"`
		UserID string `json:"user_id"`
	} `json:"users"`
}

type fixtures struct {
	Canary         string                                     `json:"canary"`
	NearDuplicates [][]string                                 `json:"near_duplicates"`
	KnowledgeBases map[string][]struct{ Key, Content string } `json:"knowledge_bases"`
	Queries        []struct {
		KB   string   `json:"kb"`
		Q    string   `json:"q"`
		Want []string `json:"want"`
		Kind string   `json:"kind"`
	} `json:"queries"`
}

func dir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(f))
}

func loadWorld(t *testing.T) (*world, *fixtures) {
	t.Helper()
	path := os.Getenv("MEMV2_E2E_SEED")
	if path == "" {
		path = filepath.Join(dir(), "seed-output.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no seed output (%v): run e2e/memory_v2/stack.sh up && go run ./e2e/memory_v2/seed", err)
	}
	var w world
	require.NoError(t, json.Unmarshal(raw, &w))
	raw, err = os.ReadFile(filepath.Join(dir(), "fixtures/memories.json"))
	require.NoError(t, err)
	var fx fixtures
	require.NoError(t, json.Unmarshal(raw, &fx))
	return &w, &fx
}

func connect(t *testing.T, w *world, endpoint string) *Session {
	t.Helper()
	ep, ok := w.MCP[endpoint]
	require.True(t, ok, "endpoint %s not seeded", endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Connect(ctx, w.BaseURL, ep.Path, ep.Token)
	require.NoError(t, err, "connect %s", endpoint)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return c
}
