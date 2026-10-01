package middleware

import (
	"strings"
	"testing"
)

// The admin token-retrieve endpoint returns the plaintext bearer token in
// {"data":{"token":...}}; the request logger must never persist it.
func TestSanitizeBodyRedactsMCPEndpointRetrievedToken(t *testing.T) {
	body := `{"success":true,"data":{"token":"mcp_supersecret123","retrievable":true}}`
	got := sanitizeBody(body)
	if strings.Contains(got, "mcp_supersecret123") {
		t.Fatalf("retrieved MCP endpoint token leaked into logged body: %s", got)
	}
}
