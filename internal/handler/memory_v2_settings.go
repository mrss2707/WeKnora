package handler

import (
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ---------------------------------------------------------------------------
// GET /api/v1/memories/settings?kb_id=   (Viewer)
// PUT /api/v1/memories/settings          (Admin of the knowledge base)
// ---------------------------------------------------------------------------

// GetMemoryModelSettings reports a knowledge base's Memory V2 model choices
// and the model each task uses now.
func (h *MemoryV2Handler) GetMemoryModelSettings(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	if kbID == "" {
		c.Error(errors.NewValidationError("kb_id is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}
	svc, ok := h.memorySvc.(interfaces.MemoryV2ModelSettingsService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "error": "memory model settings are not supported"})
		return
	}
	settings, err := svc.GetModelSettings(ctx, tenantID, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"operation": "memory_model_settings"})
		c.Error(errors.NewInternalServerError("Failed to load memory model settings"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

// UpdateMemoryModelSettings replaces both choices; an empty id restores the
// knowledge base's summary model for that task.
func (h *MemoryV2Handler) UpdateMemoryModelSettings(c *gin.Context) {
	var body struct {
		KbID string `json:"kb_id"`
		types.MemoryV2ModelSettingsUpdate
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(errors.NewValidationError("invalid request body"))
		return
	}
	kbID := strings.TrimSpace(body.KbID)
	if kbID == "" {
		c.Error(errors.NewValidationError("kb_id is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleAdmin)
	if !ok {
		return
	}
	svc, ok := h.memorySvc.(interfaces.MemoryV2ModelSettingsService)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "error": "memory model settings are not supported"})
		return
	}
	userID, _ := h.getUserID(c)
	settings, err := svc.UpdateModelSettings(ctx, tenantID, kbID, userID, body.MemoryV2ModelSettingsUpdate)
	if stderrors.Is(err, types.ErrMemoryModelSettingInvalid) {
		c.Error(errors.NewValidationError("Choose an active chat model of this workspace"))
		return
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"operation": "memory_model_settings_update"})
		c.Error(errors.NewInternalServerError("Failed to save memory model settings"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}
