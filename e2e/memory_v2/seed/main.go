// Command seed provisions the Memory V2 end-to-end world through the public
// REST API only, and writes seed-output.json for the MCP and Playwright
// suites. It is idempotent: re-running reuses accounts, workspaces, spaces,
// models and knowledge bases by name.
//
//	go run ./e2e/memory_v2/seed -base http://localhost:18080
//
// World:
//
//	Acme  (alice owner, bob admin, carol contributor, dave viewer)
//	      KBs acme-eng (shared to Partners as editor), acme-hr (viewer)
//	Beta  (erin owner)  KB beta-ops; member of Partners
//	Gamma (frank owner) KB gamma-private; isolation canary, not in Partners
//
// Models are OpenAI-compatible endpoints (Ollama by default).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	c "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
)

const password = "Memv2-e2e!"

type fixtureFile struct {
	Canary         string                     `json:"canary"`
	NearDuplicates [][]string                 `json:"near_duplicates"`
	KnowledgeBases map[string][]fixtureMemory `json:"knowledge_bases"`
	Queries        []map[string]any           `json:"queries"`
	Contradictions [][]string                 `json:"contradictions"`
}

type fixtureMemory struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

// Output is seed-output.json.
type Output struct {
	BaseURL        string                       `json:"base_url"`
	Password       string                       `json:"password"`
	Users          map[string]UserOut           `json:"users"`
	Tenants        map[string]uint64            `json:"tenants"`
	Organization   string                       `json:"organization_id"`
	KnowledgeBases map[string]KBOut             `json:"knowledge_bases"`
	Memories       map[string]map[string]string `json:"memories"` // kb -> key -> id
	MCP            map[string]MCPOut            `json:"mcp_endpoints"`
	Canary         string                       `json:"canary"`
	SeededAt       time.Time                    `json:"seeded_at"`
}

type UserOut struct {
	Email  string `json:"email"`
	UserID string `json:"user_id"`
	Tenant string `json:"tenant"`
	Role   string `json:"role"`
}

type KBOut struct {
	ID       string `json:"id"`
	TenantID uint64 `json:"tenant_id"`
	Owner    string `json:"owner"`
	Share    string `json:"share,omitempty"`
}

type MCPOut struct {
	ID     string   `json:"id"`
	Path   string   `json:"path"`
	Token  string   `json:"token"`
	Tenant string   `json:"tenant"`
	KBs    []string `json:"knowledge_bases"`
	Tools  []string `json:"tools"`
}

var (
	base     = flag.String("base", envOr("MEMV2_E2E_BASE_URL", "http://localhost:18080"), "API base URL")
	modelURL = flag.String("model-url", envOr("MEMV2_E2E_MODEL_URL", "http://localhost:11434/v1"), "OpenAI-compatible model endpoint")
	embed    = flag.String("embed-model", envOr("MEMV2_E2E_EMBED_MODEL", "bge-m3"), "embedding model")
	embedDim = flag.Int("embed-dim", 1024, "embedding dimension")
	chat     = flag.String("chat-model", envOr("MEMV2_E2E_CHAT_MODEL", "qwen2.5:3b"), "chat model")
	out      = flag.String("out", "", "output file (default: e2e/memory_v2/seed-output.json)")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	flag.Parse()
	ctx := context.Background()
	root := repoRoot()
	if *out == "" {
		*out = filepath.Join(root, "e2e/memory_v2/seed-output.json")
	}
	var fx fixtureFile
	raw, err := os.ReadFile(filepath.Join(root, "e2e/memory_v2/fixtures/memories.json"))
	must(err)
	must(json.Unmarshal(raw, &fx))

	res := &Output{
		BaseURL: *base, Password: password, Users: map[string]UserOut{}, Tenants: map[string]uint64{},
		KnowledgeBases: map[string]KBOut{}, Memories: map[string]map[string]string{}, MCP: map[string]MCPOut{},
		Canary: fx.Canary, SeededAt: time.Now(),
	}

	// Accounts: every user gets a personal workspace on registration; the
	// owners' workspaces are Acme, Beta and Gamma.
	users := map[string]*c.Session{}
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin", "frank"} {
		s, err := c.Register(ctx, *base, name, name+"@memv2-e2e.test", password)
		must(err)
		users[name] = s
	}
	tenants := map[string]uint64{"acme": users["alice"].HomeTenant, "beta": users["erin"].HomeTenant, "gamma": users["frank"].HomeTenant}
	res.Tenants = tenants

	alice := users["alice"].As(*base)
	for name, role := range map[string]string{"bob": "admin", "carol": "contributor", "dave": "viewer"} {
		must(alice.AddTenantMember(ctx, tenants["acme"], name+"@memv2-e2e.test", role))
		res.Users[name] = UserOut{Email: name + "@memv2-e2e.test", UserID: users[name].UserID, Tenant: "acme", Role: role}
	}
	res.Users["alice"] = UserOut{Email: "alice@memv2-e2e.test", UserID: users["alice"].UserID, Tenant: "acme", Role: "owner"}
	res.Users["erin"] = UserOut{Email: "erin@memv2-e2e.test", UserID: users["erin"].UserID, Tenant: "beta", Role: "owner"}
	res.Users["frank"] = UserOut{Email: "frank@memv2-e2e.test", UserID: users["frank"].UserID, Tenant: "gamma", Role: "owner"}

	// Models and knowledge bases per owner workspace.
	kbOwners := map[string]string{"acme-eng": "alice", "acme-hr": "alice", "beta-ops": "erin", "gamma-private": "frank"}
	modelIDs := map[string][2]string{}
	for _, owner := range []string{"alice", "erin", "frank"} {
		cl := users[owner].As(*base)
		e, err := ensureModel(ctx, cl, c.ModelSpec{Name: *embed, Type: "Embedding", BaseURL: *modelURL, APIKey: "e2e", Dimension: *embedDim})
		must(err)
		ch, err := ensureModel(ctx, cl, c.ModelSpec{Name: *chat, Type: "KnowledgeQA", BaseURL: *modelURL, APIKey: "e2e"})
		must(err)
		modelIDs[owner] = [2]string{e, ch}
	}
	for kbName, owner := range kbOwners {
		cl := users[owner].As(*base)
		kb, err := ensureKB(ctx, cl, kbName, modelIDs[owner][0], modelIDs[owner][1])
		must(err)
		res.KnowledgeBases[kbName] = KBOut{ID: kb.ID, TenantID: kb.TenantID, Owner: owner}
	}

	// Partners space = Acme + Beta. acme-eng shared as editor, acme-hr as viewer.
	org, err := ensureOrganization(ctx, alice, "Partners")
	must(err)
	res.Organization = org.ID
	must(users["erin"].As(*base).JoinOrganization(ctx, org.InviteCode))
	// Invite-code joins start as viewer; the effective share permission is
	// min(share, org role), so Beta needs the editor org role to write.
	must(alice.Do(ctx, "PUT", fmt.Sprintf("/api/v1/organizations/%s/members/%d", org.ID, tenants["beta"]),
		map[string]string{"role": "editor"}, nil))
	for kb, perm := range map[string]string{"acme-eng": "editor", "acme-hr": "viewer"} {
		must(alice.ShareKnowledgeBase(ctx, res.KnowledgeBases[kb].ID, org.ID, perm))
		k := res.KnowledgeBases[kb]
		k.Share = perm
		res.KnowledgeBases[kb] = k
	}

	// Memories through the public save pipeline (embedding, dedup, linking).
	// The designated near-duplicate's second half is saved by the tests.
	skip := map[string]bool{}
	for _, pair := range fx.NearDuplicates {
		skip[pair[1]] = true
	}
	kbNames := make([]string, 0, len(fx.KnowledgeBases))
	for k := range fx.KnowledgeBases {
		kbNames = append(kbNames, k)
	}
	sort.Strings(kbNames)
	for _, kbName := range kbNames {
		kb := res.KnowledgeBases[kbName]
		cl := users[kb.Owner].As(*base)
		ids := map[string]string{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, m := range fx.KnowledgeBases[kbName] {
			if skip[m.Key] {
				continue
			}
			wg.Add(1)
			go func(m fixtureMemory) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				r, err := cl.SaveMemory(ctx, kb.ID, m.Content)
				if err != nil {
					log.Fatalf("save %s/%s: %v", kbName, m.Key, err)
				}
				mu.Lock()
				ids[m.Key] = r.Memory.ID
				mu.Unlock()
			}(m)
		}
		wg.Wait()
		res.Memories[kbName] = ids
		log.Printf("seeded %s: %d memories", kbName, len(ids))
	}

	// Explicit protected fixtures, set through the same versioned human API
	// as the UI. This makes worker invariants independent of heuristics.
	for key, verdict := range map[string]string{"decision_outbox": "decision", "bug_zip_slip": "fixed"} {
		id := res.Memories["acme-eng"][key]
		m, err := alice.GetMemory(ctx, id)
		must(err)
		_, err = alice.PatchMemory(ctx, id, map[string]any{
			"verdict": verdict, "tier": 0, "expected_updated_at": m.UpdatedAt,
		})
		must(err)
	}

	// MCP endpoints.
	const fast = 6000 // tests share endpoints; only acme-rate exercises limiting
	memTools := []string{"memory_recall", "memory_graph", "memory_detail", "memory_status"}
	rw := append(append([]string{}, memTools...), "memory_save")
	endpoints := []struct {
		name, owner, tenant string
		kbs                 []string
		tools               []string
		rate                int
	}{
		{"acme-rw", "alice", "acme", nil, rw, fast},
		{"acme-ro", "alice", "acme", nil, memTools, fast},
		{"acme-eng-only", "alice", "acme", []string{"acme-eng"}, rw, fast},
		{"acme-rate", "alice", "acme", []string{"acme-eng"}, rw, 5},
		{"acme-disabled", "alice", "acme", nil, rw, fast},
		{"beta-rw", "erin", "beta", nil, rw, fast},
		// An unrestricted endpoint covers its own workspace's KBs; KBs shared
		// from another workspace must be named on the endpoint.
		{"beta-shared", "erin", "beta", []string{"acme-eng", "acme-hr"}, rw, fast},
		{"gamma-rw", "frank", "gamma", nil, rw, fast},
	}
	for _, e := range endpoints {
		kbIDs := []string{}
		for _, k := range e.kbs {
			kbIDs = append(kbIDs, res.KnowledgeBases[k].ID)
		}
		ep, err := ensureMCPEndpoint(ctx, users[e.owner].As(*base), "memv2-e2e-"+e.name, kbIDs, e.tools, e.rate)
		must(err)
		if e.name == "acme-disabled" {
			must(users[e.owner].As(*base).SetMCPEndpointEnabled(ctx, ep.ID, false))
		}
		res.MCP[e.name] = MCPOut{ID: ep.ID, Path: ep.Path, Token: ep.Token, Tenant: e.tenant, KBs: e.kbs, Tools: e.tools}
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	must(os.WriteFile(*out, b, 0o600))
	log.Printf("wrote %s", *out)
}

// ensureMCPEndpoint reuses an endpoint by name (rotating its token, which is
// only shown once) or creates it.
func ensureMCPEndpoint(ctx context.Context, cl *c.Client, name string, kbIDs, tools []string, rate int) (*c.MCPEndpoint, error) {
	var resp struct {
		Data []struct{ ID, Name, Path string }
	}
	if err := cl.Do(ctx, "GET", "/api/v1/mcp-endpoints", nil, &resp); err != nil {
		return nil, err
	}
	for _, ep := range resp.Data {
		if ep.Name != name {
			continue
		}
		if err := cl.Do(ctx, "PUT", "/api/v1/mcp-endpoints/"+ep.ID, map[string]any{
			"enabled": true, "knowledge_base_ids": kbIDs, "tools": tools, "rate_limit_per_minute": rate,
		}, nil); err != nil {
			return nil, err
		}
		token, err := cl.RotateMCPEndpointToken(ctx, ep.ID)
		return &c.MCPEndpoint{ID: ep.ID, Path: ep.Path, Token: token}, err
	}
	return cl.CreateMCPEndpoint(ctx, name, kbIDs, tools, rate)
}

func ensureModel(ctx context.Context, cl *c.Client, m c.ModelSpec) (string, error) {
	var resp struct {
		Data []struct{ ID, Name, Type string }
	}
	if err := cl.Do(ctx, "GET", "/api/v1/models", nil, &resp); err != nil {
		return "", err
	}
	for _, x := range resp.Data {
		if x.Name == m.Name && x.Type == m.Type {
			return x.ID, nil
		}
	}
	return cl.CreateModel(ctx, m)
}

func ensureKB(ctx context.Context, cl *c.Client, name, embedID, chatID string) (*c.KnowledgeBase, error) {
	kbs, err := cl.ListKnowledgeBases(ctx)
	if err != nil {
		return nil, err
	}
	for _, kb := range kbs {
		if kb.Name == name {
			return &kb, nil
		}
	}
	return cl.CreateKnowledgeBase(ctx, name, embedID, chatID)
}

func ensureOrganization(ctx context.Context, cl *c.Client, name string) (*c.Organization, error) {
	var resp struct {
		Data json.RawMessage
	}
	if err := cl.Do(ctx, "GET", "/api/v1/organizations", nil, &resp); err != nil {
		return nil, err
	}
	var list []c.Organization
	if json.Unmarshal(resp.Data, &list) != nil {
		var wrapped struct {
			Organizations []c.Organization `json:"organizations"`
		}
		_ = json.Unmarshal(resp.Data, &wrapped)
		list = wrapped.Organizations
	}
	for _, o := range list {
		if o.Name == name && o.InviteCode != "" {
			return &o, nil
		}
	}
	return cl.CreateOrganization(ctx, name)
}

func repoRoot() string {
	dir, _ := os.Getwd()
	for d := dir; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil && strings.Contains(mustRead(filepath.Join(d, "go.mod")), "module github.com/Tencent/WeKnora\n") {
			return d
		}
	}
	return dir
}

func mustRead(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func must(err error) {
	if err != nil {
		log.Fatal(fmt.Errorf("seed: %w", err))
	}
}
