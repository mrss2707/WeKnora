package interfaces

import "context"

// MCPEndpointTokenRetriever is an optional capability of MCPEndpointService
// (develop-only module mcp_attribution). It is a separate interface so the core
// MCPEndpointService contract stays identical to main; callers type-assert.
type MCPEndpointTokenRetriever interface {
	// RetrieveToken returns the decrypted plaintext bearer token for owner
	// auto-fill. Returns "" (no error) for legacy rows written before token
	// auto-fill stored no ciphertext.
	RetrieveToken(ctx context.Context, tenantID uint64, id string) (string, error)
}
