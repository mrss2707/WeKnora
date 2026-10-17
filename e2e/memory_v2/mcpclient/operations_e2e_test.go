//go:build memory_e2e

package mcpclient

import (
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/stretchr/testify/require"
)

// TestScheduledDreamerSafety is opt-in because it needs a short worker interval
// and must not compete with UI tests checking optimistic versions.
func TestScheduledDreamerSafety(t *testing.T) {
	if os.Getenv("MEMV2_E2E_SCHEDULED") != "1" {
		t.Skip("restart isolated app with MEMORY_V2_DREAMER_INTERVAL=5s and set MEMV2_E2E_SCHEDULED=1")
	}
	w, _ := loadWorld(t)
	c := ctx(t)
	session, err := rest.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
	require.NoError(t, err)
	cl := session.As(w.BaseURL)
	kb := w.KnowledgeBases["acme-eng"].ID
	snapshot := func() map[string]rest.Memory {
		result := map[string]rest.Memory{}
		for page := 1; ; page++ {
			list, err := cl.ListMemories(c, url.Values{"kb_id": {kb}, "page": {fmt.Sprint(page)}, "page_size": {"100"}, "verdicts": {"none,fixed,refuted,decision,gotcha,wip"}})
			require.NoError(t, err)
			for _, m := range list.Items {
				result[m.ID] = m
				require.Equal(t, kb, m.KbID)
			}
			if int64(page*100) >= list.Total {
				return result
			}
		}
	}
	before := snapshot()
	require.GreaterOrEqual(t, len(before), 100)
	protected := 0
	for _, m := range before {
		if m.Verdict == "decision" || m.Verdict == "fixed" {
			protected++
		}
	}
	require.GreaterOrEqual(t, protected, 2)
	status := func() int {
		var result struct {
			Data struct {
				Runs     int    `json:"runs"`
				Interval string `json:"interval"`
			}
		}
		require.NoError(t, cl.Do(c, "GET", "/api/v1/memories/dream?kb_id="+kb, nil, &result))
		interval, err := time.ParseDuration(result.Data.Interval)
		require.NoError(t, err)
		require.LessOrEqual(t, interval, 10*time.Second, "use a short interval only on the isolated E2E stack")
		return result.Data.Runs
	}
	initial := status()
	sentinel := w.Memories["acme-eng"]["vi_minio"]
	original, err := cl.GetMemory(c, sentinel)
	require.NoError(t, err)
	t.Cleanup(func() {
		current, err := cl.GetMemory(c, sentinel)
		if err != nil {
			t.Errorf("restore sentinel: %v", err)
			return
		}
		_, err = cl.PatchMemory(c, sentinel, map[string]any{"tags": original.Tags, "expected_updated_at": current.UpdatedAt})
		if err != nil {
			t.Errorf("restore sentinel tags: %v", err)
		}
	})
	for pass := 1; pass <= 3; pass++ {
		m, err := cl.GetMemory(c, sentinel)
		require.NoError(t, err)
		_, err = cl.PatchMemory(c, sentinel, map[string]any{"tags": append(m.Tags, fmt.Sprintf("scheduled-%d-%d", time.Now().UnixNano(), pass)), "expected_updated_at": m.UpdatedAt})
		require.NoError(t, err)
		require.Eventually(t, func() bool { return status() >= initial+pass }, 45*time.Second, time.Second, "scheduled pass %d", pass)
	}
	after := snapshot()
	for id, old := range before {
		current, exists := after[id]
		if old.Verdict == "decision" || old.Verdict == "fixed" {
			require.True(t, exists, "protected %s disappeared", id)
			require.Equal(t, old.Content, current.Content)
			require.Equal(t, old.Verdict, current.Verdict)
			require.Equal(t, old.Importance, current.Importance)
			require.Equal(t, old.Tier, current.Tier)
		}
		if exists {
			require.Equal(t, old.KbID, current.KbID)
			require.Equal(t, old.TenantID, current.TenantID)
		} else {
			var history struct { Data struct { Items []rest.AuditEntry } }
			require.NoError(t, cl.Do(c, "GET", "/api/v1/memories/history?kb_id="+kb+"&memory_id="+id+"&page_size=1", nil, &history))
			require.Len(t, history.Data.Items, 1)
			require.Equal(t, "merged_into", history.Data.Items[0].Action, "a scheduled dream must not delete or move a row except as an audited merge loser")
		}
	}
	t.Logf("scheduled runs: %d -> %d; live rows %d -> %d; protected rows unchanged: %d", initial, status(), len(before), len(after), protected)
}
