package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestTenantAPIKeyCreateRecordsHumanCreator(t *testing.T) {
	svc := NewTenantAPIKeyService(newFakeTenantAPIKeyRepo())
	human := "550e8400-e29b-41d4-a716-446655440000"
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, human)

	res, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{TenantID: 42, Name: "dev-key"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.APIKey.CreatedBy != human {
		t.Fatalf("CreatedBy = %q, want %q", res.APIKey.CreatedBy, human)
	}
}

func TestTenantAPIKeyCreateSkipsMachineCreators(t *testing.T) {
	svc := NewTenantAPIKeyService(newFakeTenantAPIKeyRepo())
	for _, id := range []string{"", "system-42", "mcp-ep-1", "api_platform:9"} {
		ctx := context.Background()
		if id != "" {
			ctx = context.WithValue(ctx, types.UserIDContextKey, id)
		}
		res, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{TenantID: 42, Name: "k"})
		if err != nil {
			t.Fatalf("create(%q): %v", id, err)
		}
		if res.APIKey.CreatedBy != "" {
			t.Fatalf("creator %q must not be recorded, got %q", id, res.APIKey.CreatedBy)
		}
	}
}
