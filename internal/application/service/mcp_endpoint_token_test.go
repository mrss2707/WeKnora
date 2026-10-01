package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

func TestMCPEndpointRetrieveTokenRoundTrip(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	repo := newStubMCPEndpointRepo()
	svc := newMCPEndpointServiceForTest(repo, nil)

	ep, token, err := svc.Create(context.Background(), 7, &types.MCPEndpoint{
		Name: "docs", Enabled: true, Tools: types.StringArray{types.MCPEndpointToolAsk},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The stub repo bypasses GORM hooks; run BeforeSave as the real repo would.
	row := repo.rows[ep.ID]
	if err := row.BeforeSave(nil); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if !strings.HasPrefix(row.TokenEncrypted, utils.EncPrefix) {
		t.Fatalf("token must be stored encrypted, got %q", row.TokenEncrypted)
	}

	got, err := svc.RetrieveToken(context.Background(), 7, ep.ID)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if got != token {
		t.Fatalf("retrieved %q, want %q", got, token)
	}
}

func TestMCPEndpointRetrieveTokenIsTenantScoped(t *testing.T) {
	repo := newStubMCPEndpointRepo()
	svc := newMCPEndpointServiceForTest(repo, nil)
	ep, _, err := svc.Create(context.Background(), 7, &types.MCPEndpoint{
		Name: "docs", Enabled: true, Tools: types.StringArray{types.MCPEndpointToolAsk},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.RetrieveToken(context.Background(), 8, ep.ID); !errors.Is(err, ErrMCPEndpointNotFound) {
		t.Fatalf("foreign tenant must get not-found, got %v", err)
	}
}

func TestMCPEndpointRetrieveTokenLegacyRowIsBlank(t *testing.T) {
	repo := newStubMCPEndpointRepo()
	repo.rows["legacy"] = &types.MCPEndpoint{ID: "legacy", TenantID: 7, Enabled: true}
	svc := newMCPEndpointServiceForTest(repo, nil)

	got, err := svc.RetrieveToken(context.Background(), 7, "legacy")
	if err != nil || got != "" {
		t.Fatalf("legacy row: got %q err %v, want blank/no error", got, err)
	}
}

func TestMCPEndpointRetrieveTokenMissingKey(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	enc, err := utils.EncryptAESGCM("mcp_secret", utils.GetAESKey())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYSTEM_AES_KEY", "") // key later removed/rotated

	repo := newStubMCPEndpointRepo()
	repo.rows["e"] = &types.MCPEndpoint{ID: "e", TenantID: 7, TokenEncrypted: enc}
	svc := newMCPEndpointServiceForTest(repo, nil)

	if _, err := svc.RetrieveToken(context.Background(), 7, "e"); !errors.Is(err, utils.ErrEncryptedDataMissingKey) {
		t.Fatalf("want ErrEncryptedDataMissingKey, got %v", err)
	}
}
