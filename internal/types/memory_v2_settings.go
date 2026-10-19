package types

import (
	"errors"
	"time"
)

// MemoryModelPurpose names a Memory V2 task whose chat model can be chosen per
// knowledge base.
type MemoryModelPurpose string

const (
	// MemoryModelPurposeDreamer is the background Dreamer review.
	MemoryModelPurposeDreamer MemoryModelPurpose = "dreamer"
	// MemoryModelPurposeText covers the other Memory V2 text tasks (entity extraction).
	MemoryModelPurposeText MemoryModelPurpose = "text"
)

// Sources of an effective Memory V2 model.
const (
	MemoryModelSourceSetting       = "setting"
	MemoryModelSourceKBSummary     = "kb_summary"
	MemoryModelSourceTenantDefault = "tenant_default"
	MemoryModelSourceUnavailable   = "unavailable"
)

// ErrMemoryModelUnavailable reports that a model chosen in the Memory V2
// settings no longer exists, is inactive or is not a chat model.
var ErrMemoryModelUnavailable = errors.New("memory V2: configured model unavailable")

// ErrMemoryModelSettingInvalid rejects a model that is not an active chat
// model of the knowledge base owner's workspace (or a built-in one).
var ErrMemoryModelSettingInvalid = errors.New("memory V2: model must be an active chat model of this workspace")

// MemoryV2ScopeSettings is the stored per-(tenant, KB) model choice. An empty
// id means "use the knowledge base's summary model".
type MemoryV2ScopeSettings struct {
	TenantID       string    `json:"tenant_id" gorm:"column:tenant_id;primaryKey"`
	KbID           string    `json:"kb_id" gorm:"column:kb_id;primaryKey"`
	DreamerModelID string    `json:"dreamer_model_id" gorm:"column:dreamer_model_id"`
	TextModelID    string    `json:"text_model_id" gorm:"column:text_model_id"`
	UpdatedBy      string    `json:"updated_by" gorm:"column:updated_by"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"column:updated_at"`
}

// TableName binds the settings to the module table (migration 900084).
func (MemoryV2ScopeSettings) TableName() string { return "memory_v2_scope_settings" }

// ModelFor returns the configured model id of a purpose.
func (s *MemoryV2ScopeSettings) ModelFor(purpose MemoryModelPurpose) string {
	if s == nil {
		return ""
	}
	if purpose == MemoryModelPurposeDreamer {
		return s.DreamerModelID
	}
	return s.TextModelID
}

// MemoryEffectiveModel is the model a purpose resolves to right now.
type MemoryEffectiveModel struct {
	ModelID string `json:"model_id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
}

// MemoryV2ModelSettings is the settings API payload.
type MemoryV2ModelSettings struct {
	KbID           string               `json:"kb_id"`
	DreamerModelID string               `json:"dreamer_model_id"`
	TextModelID    string               `json:"text_model_id"`
	UpdatedAt      *time.Time           `json:"updated_at,omitempty"`
	Dreamer        MemoryEffectiveModel `json:"dreamer"`
	Text           MemoryEffectiveModel `json:"text"`
}

// MemoryV2ModelSettingsUpdate replaces both choices; "" clears a choice.
type MemoryV2ModelSettingsUpdate struct {
	DreamerModelID string `json:"dreamer_model_id"`
	TextModelID    string `json:"text_model_id"`
}
