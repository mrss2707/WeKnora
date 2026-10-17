//go:build memory_e2e

// Memory V2 MCP end-to-end scenarios against a seeded stack:
//
//	e2e/memory_v2/stack.sh up && go run ./e2e/memory_v2/seed
//	go test -tags memory_e2e -count=1 -v ./e2e/memory_v2/mcpclient/
package mcpclient

import (
	"context"
	"crypto/rand"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
)

var memoryTools = []string{"memory_recall", "memory_graph", "memory_detail", "memory_status"}

func TestToolsPerProfile(t *testing.T) {
	w, _ := loadWorld(t)
	rw, err := connect(t, w, "acme-rw").Tools(ctx(t))
	require.NoError(t, err)
	for _, tool := range append(memoryTools, "memory_save") {
		assert.Contains(t, rw, tool)
	}
	ro, err := connect(t, w, "acme-ro").Tools(ctx(t))
	require.NoError(t, err)
	for _, tool := range memoryTools {
		assert.Contains(t, ro, tool)
	}
	assert.NotContains(t, ro, "memory_save", "a read-only endpoint never exposes memory_save")
}

func TestStatus(t *testing.T) {
	w, _ := loadWorld(t)
	st, err := connect(t, w, "acme-rw").Status(ctx(t))
	require.NoError(t, err)
	assert.Equal(t, true, st["available"], "%v", st)
}

func TestSaveRecallDetailGraph(t *testing.T) {
	w, _ := loadWorld(t)
	s := connect(t, w, "acme-rw")
	c := ctx(t)
	kb := w.KnowledgeBases["acme-eng"].ID
	marker := uniqueTopic()

	saved, err := s.Save(c, kb, "Fixed: the "+marker+" importer crashed on empty CSV files because the header row was missing; it now skips empty files.")
	require.NoError(t, err)
	require.True(t, saved.Created)
	require.NotEmpty(t, saved.Memory.ID)
	cleanupMemories(t, w, saved.Memory.ID)

	rec, err := s.Recall(c, kb, marker+" importer crash", 5)
	require.NoError(t, err)
	top := []string{}
	for i, r := range rec.Results {
		if i < 3 {
			top = append(top, r.ID)
		}
	}
	assert.Contains(t, top, saved.Memory.ID, "the saved memory is in the top 3")

	det, err := s.Detail(c, kb, saved.Memory.ID)
	require.NoError(t, err)
	assert.Contains(t, det.Content, marker)

	g, err := s.Graph(c, kb, saved.Memory.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, saved.Memory.ID, g.MemoryID)
	for _, e := range g.Edges {
		assert.NotEqual(t, e.Source, e.Target, "no self loops")
	}
}

func TestExactAndNearDuplicates(t *testing.T) {
	w, fx := loadWorld(t)
	s := connect(t, w, "acme-rw")
	c := ctx(t)
	kb := w.KnowledgeBases["acme-eng"].ID

	// Exact: a seeded fact saved again is not created twice.
	content := fx.KnowledgeBases["acme-eng"][0].Content
	again, err := s.Save(c, kb, content)
	require.NoError(t, err)
	assert.False(t, again.Created, "exact duplicate")
	assert.Equal(t, w.Memories["acme-eng"][fx.KnowledgeBases["acme-eng"][0].Key], again.Memory.ID)

	// Near: the designated restatement merges into the seeded original.
	pair := fx.NearDuplicates[0]
	var restatement string
	for _, m := range fx.KnowledgeBases["acme-eng"] {
		if m.Key == pair[1] {
			restatement = m.Content
		}
	}
	require.NotEmpty(t, restatement)
	near, err := s.Save(c, kb, restatement)
	require.NoError(t, err)
	assert.False(t, near.Created, "near duplicate merges")
	assert.Equal(t, w.Memories["acme-eng"][pair[0]], near.Memory.ID)

	// Saving it again does not grow the memory: the restatement is either
	// absorbed once (near) or dropped as the same fact (exact).
	det1, err := s.Detail(c, kb, near.Memory.ID)
	require.NoError(t, err)
	again2, err := s.Save(c, kb, restatement)
	require.NoError(t, err)
	assert.Equal(t, near.Memory.ID, again2.Memory.ID)
	det2, err := s.Detail(c, kb, again2.Memory.ID)
	require.NoError(t, err)
	assert.Equal(t, det1.Content, det2.Content, "saving the same fact again never grows the memory")
	assert.LessOrEqual(t, strings.Count(det2.Content, restatement), 1, "a fact is never appended twice")
}

// TestRecallQuality checks the seeded queries through MCP: the expected
// memory is in the top 5 for at least 80% of queries (real embedding model).
func TestRecallQuality(t *testing.T) {
	w, fx := loadWorld(t)
	s := connect(t, w, "acme-rw")
	c := ctx(t)
	hits, total := 0, 0
	var misses []string
	for _, q := range fx.Queries {
		if q.KB != "acme-eng" && q.KB != "acme-hr" {
			continue
		}
		total++
		res, err := s.Recall(c, w.KnowledgeBases[q.KB].ID, q.Q, 5)
		require.NoError(t, err)
		want := map[string]bool{}
		for _, k := range q.Want {
			want[w.Memories[q.KB][k]] = true
		}
		found := false
		for _, r := range res.Results {
			found = found || want[r.ID]
		}
		if found {
			hits++
		} else {
			misses = append(misses, fmt.Sprintf("[%s] %q", q.Kind, q.Q))
		}
	}
	rate := float64(hits) / float64(total)
	t.Logf("recall@5 over MCP: %d/%d = %.2f; misses: %v", hits, total, rate, misses)
	assert.GreaterOrEqual(t, rate, 0.8)
}

func TestVietnameseCrossLingualRecall(t *testing.T) {
	w, _ := loadWorld(t)
	s := connect(t, w, "acme-rw")
	res, err := s.Recall(ctx(t), w.KnowledgeBases["acme-hr"].ID, "nghỉ phép năm bao nhiêu ngày", 5)
	require.NoError(t, err)
	ids := []string{}
	for _, r := range res.Results {
		ids = append(ids, r.ID)
	}
	hr := w.Memories["acme-hr"]
	assert.True(t, slices.Contains(ids, hr["hr_vi_nghi_phep"]) || slices.Contains(ids, hr["hr_leave"]), "VI query finds the leave policy")
}

func TestSharedKnowledgeBases(t *testing.T) {
	w, _ := loadWorld(t)
	c := ctx(t)
	beta := connect(t, w, "beta-shared")

	// Editor share: Beta reads and writes acme-eng (regression dc8b7448).
	eng := w.KnowledgeBases["acme-eng"].ID
	rec, err := beta.Recall(c, eng, "postgres connection pool", 3)
	require.NoError(t, err)
	assert.NotEmpty(t, rec.Results)
	saved, err := beta.Save(c, eng, "Beta partner note on "+uniqueTopic()+": the shared staging cluster is reachable over the partner VPN only.")
	require.NoError(t, err, "editor share can write")
	assert.True(t, saved.Created)
	cleanupMemories(t, w, saved.Memory.ID)

	// The memory lands in the owner's knowledge base, authored by erin.
	alice, err := e2eclient.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	got, err := alice.As(w.BaseURL).GetMemory(c, saved.Memory.ID)
	require.NoError(t, err, "the owner sees the partner's memory")
	assert.Equal(t, fmt.Sprint(w.KnowledgeBases["acme-eng"].TenantID), got.TenantID, "stored under the KB owner's tenant")

	// Viewer share: Beta reads acme-hr but cannot write.
	hr := w.KnowledgeBases["acme-hr"].ID
	_, err = beta.Recall(c, hr, "annual leave", 3)
	require.NoError(t, err)
	_, err = beta.Save(c, hr, "Beta should not be able to write into a knowledge base shared as viewer.")
	var te *ToolError
	require.ErrorAs(t, err, &te, "viewer share denies writes")
}

func TestIsolationCanary(t *testing.T) {
	w, fx := loadWorld(t)
	c := ctx(t)
	gamma := w.KnowledgeBases["gamma-private"].ID
	for _, ep := range []string{"acme-rw", "beta-shared"} {
		s := connect(t, w, ep)
		_, err := s.Recall(c, gamma, fx.Canary, 5)
		var te *ToolError
		require.ErrorAs(t, err, &te, "%s must not reach gamma-private", ep)
		for _, kb := range []string{"acme-eng", "acme-hr"} {
			res, err := s.Recall(c, w.KnowledgeBases[kb].ID, fx.Canary+" vault code hardware security module", 10)
			require.NoError(t, err)
			for _, r := range res.Results {
				assert.NotContains(t, r.ContentPreview, fx.Canary, "%s leaked a Gamma memory via %s", ep, kb)
			}
		}
	}
	// Gamma's own endpoint sees its canary.
	res, err := connect(t, w, "gamma-rw").Recall(c, gamma, "hardware security module vault code", 5)
	require.NoError(t, err)
	require.NotEmpty(t, res.Results)
	assert.Contains(t, res.Results[0].ContentPreview, fx.Canary)
}

func TestKBScopedEndpoint(t *testing.T) {
	w, _ := loadWorld(t)
	s := connect(t, w, "acme-eng-only")
	c := ctx(t)
	_, err := s.Recall(c, w.KnowledgeBases["acme-eng"].ID, "redis", 3)
	require.NoError(t, err)
	_, err = s.Recall(c, w.KnowledgeBases["acme-hr"].ID, "leave", 3)
	var te *ToolError
	require.ErrorAs(t, err, &te, "a KB-scoped endpoint cannot reach other knowledge bases")
}

func TestReadOnlyEndpointCannotSave(t *testing.T) {
	w, _ := loadWorld(t)
	s := connect(t, w, "acme-ro")
	_, err := s.Save(ctx(t), w.KnowledgeBases["acme-eng"].ID, "A read-only endpoint must never be able to store this memory.")
	require.Error(t, err)
}

func TestAuthFailures(t *testing.T) {
	w, _ := loadWorld(t)
	ep := w.MCP["acme-rw"]
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status := func(path, token string) int {
		req, err := http.NewRequestWithContext(c, http.MethodPost, w.BaseURL+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"auth-e2e","version":"1"}}}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	require.Equal(t, http.StatusUnauthorized, status(ep.Path, "wrong-token"))

	_, err := Connect(c, w.BaseURL, ep.Path, "wrong-token")
	require.Error(t, err, "bad token rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "authorization")

	dis := w.MCP["acme-disabled"]
	require.Equal(t, http.StatusForbidden, status(dis.Path, dis.Token))
	_, err = Connect(c, w.BaseURL, dis.Path, dis.Token)
	require.Error(t, err, "disabled endpoint rejected")

	// Rotation invalidates the old token. Uses its own endpoint copy so other
	// tests keep a valid token: rotate acme-rate and restore nothing (the
	// seed rotates again on the next run).
	alice, err := e2eclient.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	rate := w.MCP["acme-rate"]
	oldToken, err := alice.As(w.BaseURL).RotateMCPEndpointToken(c, rate.ID)
	require.NoError(t, err)
	openSession, err := Connect(c, w.BaseURL, rate.Path, oldToken)
	require.NoError(t, err)
	defer openSession.Close()
	newToken, err := alice.As(w.BaseURL).RotateMCPEndpointToken(c, rate.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, status(rate.Path, oldToken))
	_, err = openSession.Recall(c, w.KnowledgeBases["acme-eng"].ID, "redis", 1)
	require.Error(t, err, "rotation invalidates an already-initialized MCP session")
	require.True(t, strings.Contains(err.Error(), "401") || strings.Contains(strings.ToLower(err.Error()), "authorization"), "%v", err)
	require.Equal(t, http.StatusUnauthorized, status(rate.Path, rate.Token))
	_, err = Connect(c, w.BaseURL, rate.Path, rate.Token)
	require.Error(t, err, "rotated-out token rejected")
	s, err := Connect(c, w.BaseURL, rate.Path, newToken)
	require.NoError(t, err, "new token works")
	_ = s.Close()
}

func TestEndpointRateLimit(t *testing.T) {
	w, _ := loadWorld(t)
	c := ctx(t)
	alice, err := e2eclient.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	rate := w.MCP["acme-rate"]
	token, err := alice.As(w.BaseURL).RotateMCPEndpointToken(c, rate.ID)
	require.NoError(t, err)
	s, err := Connect(c, w.BaseURL, rate.Path, token)
	if err != nil {
		require.Contains(t, err.Error(), "429")
		return
	}
	defer s.Close()
	limited := false
	for i := 0; i < 12 && !limited; i++ {
		_, err := s.Recall(c, w.KnowledgeBases["acme-eng"].ID, "redis", 1)
		limited = err != nil && (strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate"))
	}
	assert.True(t, limited, "5/min endpoint is rate limited within 12 calls")
}

// TestConcurrentSaves: 50 agents saving at once never get a server error,
// and the same fact sent by 10 of them is stored once (no duplicate rows).
func TestConcurrentSaves(t *testing.T) {
	w, _ := loadWorld(t)
	c := ctx(t)
	kb := w.KnowledgeBases["acme-eng"].ID
	topic := uniqueTopic()
	shared := fmt.Sprintf("The %s service exports Prometheus metrics on the /metrics path every 15 seconds.", topic)
	distinct := []string{
		"stores session blobs in Redis with a 24 hour TTL",
		"is deployed with three replicas behind the internal load balancer",
		"retries outbound webhooks five times with jittered backoff",
		"writes structured JSON logs that are shipped to Loki",
		"reads its feature flags from Unleash at startup",
	}
	type outcome struct {
		id  string
		err error
	}
	results := make([]outcome, 50)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Connect(c, w.BaseURL, w.MCP["acme-rw"].Path, w.MCP["acme-rw"].Token)
			if err != nil {
				results[i].err = err
				return
			}
			defer s.Close()
			content := shared
			if i%5 != 0 {
				content = fmt.Sprintf("The %s worker %d %s.", topic, i, distinct[i%len(distinct)])
			}
			r, err := s.Save(c, kb, content)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].id = r.Memory.ID
		}(i)
	}
	wg.Wait()
	createdIDs := make([]string, 0, len(results))
	for _, r := range results {
		createdIDs = append(createdIDs, r.id)
	}
	cleanupMemories(t, w, createdIDs...)
	sharedIDs := map[string]bool{}
	for i, r := range results {
		require.NoError(t, r.err, "save %d", i)
		if i%5 == 0 {
			sharedIDs[r.id] = true
		}
	}
	assert.Len(t, sharedIDs, 1, "the same fact from 10 agents resolves to one memory")

	// No two live rows hold the same text.
	alice, err := e2eclient.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	seen := map[string]string{}
	for page := 1; ; page++ {
		res, err := alice.As(w.BaseURL).ListMemories(c, map[string][]string{
			"kb_id": {kb}, "keyword": {topic}, "page": {fmt.Sprint(page)}, "page_size": {"100"},
		})
		require.NoError(t, err)
		for _, m := range res.Items {
			if prev, dup := seen[m.Content]; dup {
				t.Errorf("duplicate rows %s and %s hold the same content", prev, m.ID)
			}
			seen[m.Content] = m.ID
		}
		if len(res.Items) < 100 {
			break
		}
	}
	assert.NotEmpty(t, seen, "the run's memories are listed")
}

// TestProtocolLoop simulates an agent following MEMORY_PROTOCOL_INSTRUCTION:
// status → recall → (nothing found) save → recall finds it → graph before a
// similar save → the similar save does not duplicate.
func TestProtocolLoop(t *testing.T) {
	w, _ := loadWorld(t)
	s := connect(t, w, "acme-eng-only")
	c := ctx(t)
	kb := w.KnowledgeBases["acme-eng"].ID

	st, err := s.Status(c)
	require.NoError(t, err)
	require.Equal(t, true, st["available"])

	topic := uniqueTopic()
	before, err := s.Recall(c, kb, topic+" timeout", 5)
	require.NoError(t, err)
	for _, r := range before.Results {
		require.NotContains(t, r.ContentPreview, topic)
	}

	fact := "Fixed: the " + topic + " job timed out after 30s because it loaded every invoice at once; it now streams invoices in pages of 500."
	saved, err := s.Save(c, kb, fact)
	require.NoError(t, err)
	require.True(t, saved.Created)
	cleanupMemories(t, w, saved.Memory.ID)

	after, err := s.Recall(c, kb, topic+" job timed out loading invoices", 5)
	require.NoError(t, err)
	ids := []string{}
	for _, r := range after.Results {
		ids = append(ids, r.ID)
	}
	assert.Contains(t, ids, saved.Memory.ID, "a later recall finds the saved fact")

	_, err = s.Graph(c, kb, saved.Memory.ID, 1)
	require.NoError(t, err)
	again, err := s.Save(c, kb, fact)
	require.NoError(t, err)
	assert.False(t, again.Created, "the protocol's second save is a no-op")
}

// TestRESTRoleFloors exercises the REST API with each Acme role.
func TestRESTRoleFloors(t *testing.T) {
	w, _ := loadWorld(t)
	c := ctx(t)
	kb := w.KnowledgeBases["acme-eng"].ID
	// Members act in Acme after switching workspace, like the UI does.
	as := func(name string) *e2eclient.Client {
		s, err := e2eclient.Login(c, w.BaseURL, w.Users[name].Email, w.Password)
		require.NoError(t, err)
		s, err = s.SwitchTenant(c, w.BaseURL, w.KnowledgeBases["acme-eng"].TenantID)
		require.NoError(t, err)
		return s.As(w.BaseURL)
	}
	dave, carol, bob := as("dave"), as("carol"), as("bob")

	_, err := dave.ListMemories(c, map[string][]string{"kb_id": {kb}})
	require.NoError(t, err, "viewer can read")
	_, err = dave.SaveMemory(c, kb, "Viewers must not be able to create memories in this knowledge base.")
	assert.Equal(t, http.StatusForbidden, e2eclient.StatusOf(err), "viewer cannot write: %v", err)

	saved, err := carol.SaveMemory(c, kb, "Contributor note about "+uniqueTopic()+": the build agents run on spot instances.")
	require.NoError(t, err, "contributor can write")
	require.True(t, saved.Created)
	cleanupMemories(t, w, saved.Memory.ID)
	_, err = carol.Dream(c, kb, true)
	assert.Equal(t, http.StatusForbidden, e2eclient.StatusOf(err), "contributor cannot dream")

	_, err = bob.Dream(c, kb, true)
	require.NoError(t, err, "admin can preview a dream pass")

	// Optimistic locking: a stale write is rejected with 409.
	m, err := carol.GetMemory(c, saved.Memory.ID)
	require.NoError(t, err)
	bm, err := bob.PatchMemory(c, m.ID, map[string]any{"importance": 3, "expected_updated_at": m.UpdatedAt})
	require.NoError(t, err)
	require.True(t, bm.UpdatedAt.After(m.UpdatedAt), "bob's write moves the version")
	_, err = carol.PatchMemory(c, m.ID, map[string]any{"importance": 1, "expected_updated_at": m.UpdatedAt})
	assert.Equal(t, http.StatusConflict, e2eclient.StatusOf(err), "stale write rejected: %v", err)

	// Embeddings never leave the server.
	got, err := dave.GetMemory(c, m.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Embedding)

	// Gamma is invisible to Acme users over REST too.
	_, err = bob.ListMemories(c, map[string][]string{"kb_id": {w.KnowledgeBases["gamma-private"].ID}})
	assert.Equal(t, http.StatusForbidden, e2eclient.StatusOf(err), "%v", err)
}

// uniqueTopic returns a made-up four-word subject, different on every call
// (a bare number would make two runs' facts near-duplicates that dedup
// rightly merges).
func uniqueTopic() string {
	words := []string{"walrus", "saffron", "glacier", "tambourine", "nebula", "origami", "cactus", "lighthouse",
		"marzipan", "volcano", "harpsichord", "mangrove", "tundra", "kaleidoscope", "porcupine", "zeppelin",
		"quasar", "avalanche", "bonsai", "catamaran", "dandelion", "escalator", "flamingo", "gondola",
		"hedgehog", "igloo", "jackfruit", "kiwano", "lagoon", "monsoon", "narwhal", "obsidian"}
	perm := mrand.New(mrand.NewPCG(uint64(time.Now().UnixNano()), cryptoUint64())).Perm(len(words))
	return strings.Join([]string{words[perm[0]], words[perm[1]], words[perm[2]], words[perm[3]]}, " ")
}

func cryptoUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}
