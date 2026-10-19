//go:build memory_e2e

package quality

import (
	"net/http"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
)

// dreamClient waits longer than the server's five-minute model-call deadline,
// so a hung provider surfaces as the server's pre-mutation "LLM call failed"
// error (safe to retry) instead of a client timeout with an unknown outcome.
func dreamClient(c *rest.Client) *rest.Client {
	copy := *c
	copy.HTTP = &http.Client{Timeout: 6 * time.Minute}
	return &copy
}
