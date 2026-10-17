//go:build memory_e2e

package mcpclient

import (
	"context"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
)

// Delete only IDs returned by this test, and never any seeded fixture. Repeated
// suites must not accumulate random distractors and change recall baselines.
func cleanupMemories(t *testing.T, w *world, ids ...string) {
	t.Helper()
	seeded := map[string]bool{}
	for _, memories := range w.Memories {
		for _, id := range memories {
			seeded[id] = true
		}
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		session, err := rest.Login(c, w.BaseURL, w.Users["alice"].Email, w.Password)
		if err != nil {
			t.Errorf("cleanup login: %v", err)
			return
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] || seeded[id] {
				continue
			}
			seen[id] = true
			if err := session.As(w.BaseURL).DeleteMemory(c, id); err != nil && rest.StatusOf(err) != 404 {
				t.Errorf("cleanup test memory %s: %v", id, err)
			}
		}
	})
}
