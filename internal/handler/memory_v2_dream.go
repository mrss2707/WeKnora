package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ---------------------------------------------------------------------------
// GET /api/v1/memories/dream?kb_id=
// ---------------------------------------------------------------------------

// GetDreamStatus reports a knowledge base's dreamer settings and last run.
func (h *MemoryV2Handler) GetDreamStatus(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	if kbID == "" {
		c.Error(errors.NewValidationError("kb_id is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}
	reader, ok := h.memorySvc.(interfaces.MemoryV2DreamStatusReader)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": &types.DreamStatus{}})
		return
	}
	status, err := reader.DreamStatus(ctx, tenantID, kbID)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to load dreamer status", tenantID, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}
