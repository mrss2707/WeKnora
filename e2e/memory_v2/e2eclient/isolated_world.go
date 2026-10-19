package e2eclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

// IsolatedWorld intentionally excludes bearer tokens from the seed document.
type IsolatedWorld struct {
	verifiedBase string
	BaseURL      string `json:"base_url"`
	Password     string `json:"password"`
	Users        map[string]struct {
		Email string `json:"email"`
	} `json:"users"`
	KnowledgeBases map[string]KnowledgeBase `json:"knowledge_bases"`
}

// LoadIsolatedWorld refuses non-fixture destinations before any test writes.
func LoadIsolatedWorld(ctx context.Context) (*IsolatedWorld, error) {
	path := os.Getenv("MEMV2_E2E_SEED")
	if path == "" {
		_, source, _, _ := runtime.Caller(0)
		path = filepath.Join(filepath.Dir(source), "..", "seed-output.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("isolated seed is required: %w", err)
	}
	var world IsolatedWorld
	if err := json.Unmarshal(data, &world); err != nil {
		return nil, err
	}
	if world.BaseURL != "http://localhost:18080" && world.BaseURL != "http://localhost:18082" {
		return nil, fmt.Errorf("only the isolated host/Compose stacks are supported")
	}
	if !strings.HasSuffix(world.Users["alice"].Email, "@memv2-e2e.test") {
		return nil, fmt.Errorf("fixture owner is required")
	}
	kbID, err := uuid.Parse(world.KnowledgeBases["acme-eng"].ID)
	if err != nil {
		return nil, fmt.Errorf("fixture KB is required")
	}
	session, err := Login(ctx, world.BaseURL, world.Users["alice"].Email, world.Password)
	if err != nil {
		return nil, fmt.Errorf("fixture authentication failed")
	}
	var response struct{ Data KnowledgeBase }
	if err := session.As(world.BaseURL).Do(ctx, "GET", "/api/v1/knowledge-bases/"+kbID.String(), nil, &response); err != nil {
		return nil, fmt.Errorf("fixture KB verification failed")
	}
	if response.Data.Name != "acme-eng" {
		return nil, fmt.Errorf("refusing a non-fixture knowledge base")
	}
	world.verifiedBase = world.BaseURL
	return &world, nil
}

// Verified requires the live fixture proof and rejects a subsequently changed URL.
func (w *IsolatedWorld) Verified() bool {
	return w != nil && w.verifiedBase != "" && w.verifiedBase == w.BaseURL
}
