//go:build memory_e2e

package mcpclient

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/stretchr/testify/require"
)

// A forwarding proxy injects an upstream outage without stopping anyone's
// Ollama. Successful embeddings still come from the real local model.
func TestModelDown_SaveFailsClearlyAndRecallDegradesToKeyword(t *testing.T) {
	w, _ := loadWorld(t)
	c := ctx(t)
	session, err := rest.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	cl := session.As(w.BaseURL)
	target := os.Getenv("MEMV2_E2E_MODEL_URL")
	if target == "" {
		target = "http://localhost:11434/v1"
	}
	upstream, err := url.Parse(target)
	require.NoError(t, err)
	// Keep the /v1 prefix supplied by the registered model URL, not twice.
	upstream.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"injected model outage"}}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	model, err := cl.CreateModel(c, rest.ModelSpec{Name: envModel(), Type: "Embedding", BaseURL: server.URL + "/v1", APIKey: "e2e", Dimension: 1024})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Do(c, "DELETE", "/api/v1/models/"+model, nil, nil) })
	var original struct {
		Data struct {
			SummaryModelID string `json:"summary_model_id"`
		}
	}
	require.NoError(t, cl.Do(c, "GET", "/api/v1/knowledge-bases/"+w.KnowledgeBases["acme-eng"].ID, nil, &original))
	kb, err := cl.CreateKnowledgeBase(c, "model-outage-"+time.Now().Format("150405.000000"), model, original.Data.SummaryModelID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Do(c, "DELETE", "/api/v1/knowledge-bases/"+kb.ID, nil, nil) })
	saved, err := cl.SaveMemory(c, kb.ID, "The emergency archive compactor refuses symlink entries to prevent traversal outside its isolated extraction root.")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.DeleteMemory(c, saved.Memory.ID) })
	offline.Store(true)
	_, err = cl.SaveMemory(c, kb.ID, "A separate disaster recovery manual describes how to restore the shipping manifests.")
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, rest.StatusOf(err))
	require.Contains(t, strings.ToLower(err.Error()), "embed")
	results, err := cl.SearchMemories(c, kb.ID, "archive compactor symlink", 5)
	require.NoError(t, err)
	require.NotEmpty(t, results, "model outage must not disable keyword recall")
	require.Equal(t, saved.Memory.ID, results[0].Memory.ID)
}

func envModel() string {
	if name := os.Getenv("MEMV2_E2E_EMBED_MODEL"); name != "" {
		return name
	}
	return "bge-m3"
}
