package memory_v2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var _ interfaces.MemoryV2ModelSettingsService = (*MemoryServiceV2Impl)(nil)

// attachSettings lets the resolver read per-KB model choices when the
// repository persists them.
func (s *MemoryServiceV2Impl) attachSettings() {
	if store, ok := s.repo.(interfaces.MemoryV2ScopeSettingsStore); ok && s.resolver != nil {
		s.resolver.settings = store
	}
}

func (s *MemoryServiceV2Impl) settingsStore() (interfaces.MemoryV2ScopeSettingsStore, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2: %s", reason)
	}
	store, ok := s.repo.(interfaces.MemoryV2ScopeSettingsStore)
	if !ok {
		return nil, fmt.Errorf("memory V2: model settings are not supported by this repository")
	}
	return store, nil
}

// GetModelSettings returns the stored choices and the model each purpose
// resolves to now. The caller has authorized (tenantID, kbID).
func (s *MemoryServiceV2Impl) GetModelSettings(ctx context.Context, tenantID, kbID string) (*types.MemoryV2ModelSettings, error) {
	store, err := s.settingsStore()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(kbID) == "" {
		return nil, fmt.Errorf("memory V2: tenant and knowledge base are required")
	}
	stored, err := store.GetScopeSettings(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	out := &types.MemoryV2ModelSettings{KbID: kbID}
	if stored != nil {
		out.DreamerModelID, out.TextModelID = stored.DreamerModelID, stored.TextModelID
		updated := stored.UpdatedAt
		out.UpdatedAt = &updated
	}
	out.Dreamer = s.effectiveModel(ctx, tenantID, kbID, types.MemoryModelPurposeDreamer)
	out.Text = s.effectiveModel(ctx, tenantID, kbID, types.MemoryModelPurposeText)
	return out, nil
}

// effectiveModel never exposes a resolution error; an unusable choice is
// reported as unavailable.
func (s *MemoryServiceV2Impl) effectiveModel(ctx context.Context, tenantID, kbID string, purpose types.MemoryModelPurpose) types.MemoryEffectiveModel {
	effective, tid, err := s.resolver.chatModelFor(ctx, tenantID, kbID, purpose)
	if err != nil {
		return types.MemoryEffectiveModel{ModelID: effective.ModelID, Source: types.MemoryModelSourceUnavailable}
	}
	if effective.Name == "" {
		if model, ok := s.resolver.usableChatModel(ctx, tid, effective.ModelID); ok {
			effective.Name = modelLabel(model)
		}
	}
	return effective
}

// UpdateModelSettings validates both choices against the knowledge base
// owner's tenant, then replaces them. An empty id clears a choice.
func (s *MemoryServiceV2Impl) UpdateModelSettings(ctx context.Context, tenantID, kbID, userID string, update types.MemoryV2ModelSettingsUpdate) (*types.MemoryV2ModelSettings, error) {
	store, err := s.settingsStore()
	if err != nil {
		return nil, err
	}
	tid, err := parseTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(kbID) == "" {
		return nil, fmt.Errorf("memory V2: knowledge base is required")
	}
	settings := &types.MemoryV2ScopeSettings{
		TenantID: tenantID, KbID: kbID,
		DreamerModelID: strings.TrimSpace(update.DreamerModelID),
		TextModelID:    strings.TrimSpace(update.TextModelID),
		UpdatedBy:      userID, UpdatedAt: time.Now().UTC(),
	}
	for _, id := range []string{settings.DreamerModelID, settings.TextModelID} {
		if id == "" {
			continue
		}
		if len(id) > 64 {
			return nil, types.ErrMemoryModelSettingInvalid
		}
		if _, ok := s.resolver.usableChatModel(ctx, tid, id); !ok {
			return nil, types.ErrMemoryModelSettingInvalid
		}
	}
	if err := store.UpsertScopeSettings(ctx, settings); err != nil {
		return nil, err
	}
	return s.GetModelSettings(ctx, tenantID, kbID)
}
