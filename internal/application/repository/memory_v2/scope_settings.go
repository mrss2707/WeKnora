package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2ScopeSettingsStore = (*MemoryRepository)(nil)

// GetScopeSettings reads the exact (tenant, KB) row; a missing row is not an error.
func (r *MemoryRepository) GetScopeSettings(ctx context.Context, tenantID, kbID string) (*types.MemoryV2ScopeSettings, error) {
	if tenantID == "" || kbID == "" {
		return nil, fmt.Errorf("memory V2 settings scope is required")
	}
	var row types.MemoryV2ScopeSettings
	err := r.db.WithContext(ctx).Raw(`SELECT tenant_id, kb_id, COALESCE(dreamer_model_id, '') AS dreamer_model_id,
  COALESCE(text_model_id, '') AS text_model_id, updated_by, updated_at
  FROM memory_v2_scope_settings WHERE tenant_id = ? AND kb_id = ?`, tenantID, kbID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertScopeSettings replaces both choices of a scope; empty ids are stored as NULL.
func (r *MemoryRepository) UpsertScopeSettings(ctx context.Context, s *types.MemoryV2ScopeSettings) error {
	if s == nil || s.TenantID == "" || s.KbID == "" {
		return fmt.Errorf("memory V2 settings scope is required")
	}
	return r.db.WithContext(ctx).Exec(`INSERT INTO memory_v2_scope_settings
  (tenant_id, kb_id, dreamer_model_id, text_model_id, updated_by, updated_at)
  VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
  ON CONFLICT (tenant_id, kb_id) DO UPDATE SET dreamer_model_id = EXCLUDED.dreamer_model_id,
  text_model_id = EXCLUDED.text_model_id, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		s.TenantID, s.KbID, s.DreamerModelID, s.TextModelID, s.UpdatedBy, s.UpdatedAt).Error
}
