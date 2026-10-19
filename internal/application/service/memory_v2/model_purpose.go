package memory_v2

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// chatModelFor picks a purpose's model: the per-KB setting, then the KB's
// summary model, then the tenant's first chat model. Settings are read before
// any client cache lookup, so a change applies on the next call on every
// replica. A configured Dreamer model that is no longer usable fails closed
// (no silent switch to a model that was not chosen for semantic edits); the
// text purpose falls back to the summary model.
func (r *modelResolver) chatModelFor(ctx context.Context, tenantID, kbID string, purpose types.MemoryModelPurpose) (types.MemoryEffectiveModel, uint64, error) {
	scope, err := r.scopeModels(ctx, tenantID, kbID)
	if err != nil {
		return types.MemoryEffectiveModel{}, 0, err
	}
	if r.settings != nil && kbID != "" {
		stored, err := r.settings.GetScopeSettings(ctx, tenantID, kbID)
		if err != nil {
			return types.MemoryEffectiveModel{}, 0, fmt.Errorf("memory V2: load model settings: %w", err)
		}
		if id := stored.ModelFor(purpose); id != "" {
			if model, ok := r.usableChatModel(ctx, scope.tenantID, id); ok {
				return types.MemoryEffectiveModel{ModelID: id, Name: modelLabel(model), Source: types.MemoryModelSourceSetting}, scope.tenantID, nil
			}
			if purpose == types.MemoryModelPurposeDreamer {
				return types.MemoryEffectiveModel{ModelID: id, Source: types.MemoryModelSourceUnavailable}, scope.tenantID,
					fmt.Errorf("%w: %s", types.ErrMemoryModelUnavailable, id)
			}
			logger.Warnf(ctx, "memory V2: configured %s model %s unavailable for kb %s, using the summary model", purpose, id, kbID)
		}
	}
	if scope.chatModelID != "" {
		return types.MemoryEffectiveModel{ModelID: scope.chatModelID, Source: types.MemoryModelSourceKBSummary}, scope.tenantID, nil
	}
	id, err := r.firstModelOfType(ctx, scope.tenantID, types.ModelTypeKnowledgeQA)
	if err != nil {
		return types.MemoryEffectiveModel{}, 0, err
	}
	return types.MemoryEffectiveModel{ModelID: id, Source: types.MemoryModelSourceTenantDefault}, scope.tenantID, nil
}

// usableChatModel checks that a model is visible to the tenant (own or
// built-in), active and a chat model.
func (r *modelResolver) usableChatModel(ctx context.Context, tid uint64, id string) (*types.Model, bool) {
	model, err := r.modelService.GetModelByID(tenantCtx(ctx, tid), id)
	if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA {
		return nil, false
	}
	return model, true
}

func modelLabel(m *types.Model) string {
	if m == nil {
		return ""
	}
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.Name
}
