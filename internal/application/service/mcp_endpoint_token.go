package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

var _ interfaces.MCPEndpointTokenRetriever = (*mcpEndpointService)(nil)

// RetrieveToken returns the decrypted plaintext bearer token for an endpoint so
// a logged-in owner can auto-fill it in a config UI. It is guarded exactly like
// RotateToken (getOwned + requireKeyCoversEndpoint) so a scoped API key cannot
// extract a token broader than its own scope.
//
// Legacy rows written before token auto-fill stored no ciphertext; those return
// "" with no error so the caller can surface "not retrievable, rotate to enable".
func (s *mcpEndpointService) RetrieveToken(ctx context.Context, tenantID uint64, id string) (string, error) {
	ep, err := s.getOwned(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if err := requireKeyCoversEndpoint(ctx, ep); err != nil {
		return "", err
	}
	if strings.TrimSpace(ep.TokenEncrypted) == "" {
		return "", nil
	}
	return utils.DecryptStoredSecret(ep.TokenEncrypted)
}
