package types

import (
	"context"
	"strings"
)

// IsHumanUserID reports whether id refers to a real human rather than a
// synthetic machine principal. It rejects:
//   - empty IDs
//   - "system-<tenantID>" (X-API-Key auth path) via IsSyntheticUserID
//   - "mcp-<endpointID>" (MCP endpoint bearer path)
//   - principal StorageID forms containing ":" (e.g. "api_platform:123",
//     "api_external_user:…")
//
// Real human user IDs are opaque UUIDs, so none of the rejected shapes can
// collide with one.
func IsHumanUserID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if IsSyntheticUserID(id) {
		return false
	}
	if strings.HasPrefix(id, "mcp-") {
		return false
	}
	if strings.Contains(id, ":") {
		return false
	}
	return true
}

// HumanUserIDFromContext returns the caller's user ID only when it refers to a
// real human (see IsHumanUserID). Use this wherever code records "the creator /
// owner" of a resource so machine callers leave the field empty (treat the
// resource as tenant-owned) instead of writing a meaningless synthetic id.
func HumanUserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := UserIDFromContext(ctx)
	if !ok {
		return "", false
	}
	if !IsHumanUserID(id) {
		return "", false
	}
	return strings.TrimSpace(id), true
}

// HumanUserIDOrEmpty collapses HumanUserIDFromContext to a plain string for
// one-shot struct-literal assignments ("" when the caller is not a human).
func HumanUserIDOrEmpty(ctx context.Context) string {
	id, _ := HumanUserIDFromContext(ctx)
	return id
}
