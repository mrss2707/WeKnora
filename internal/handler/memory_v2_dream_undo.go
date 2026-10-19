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
// POST /api/v1/memories/dream/undo  {kb_id, run_id}
// ---------------------------------------------------------------------------

// UndoDreamRun reverts the changes of one Dreamer pass (Admin on the KB).
// Memories changed after the pass are left alone and listed as skipped.
func (h *MemoryV2Handler) UndoDreamRun(c *gin.Context) {
	var body struct {
		KbID  string `json:"kb_id"`
		RunID string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(errors.NewValidationError("invalid request body"))
		return
	}
	kbID, runID := strings.TrimSpace(body.KbID), strings.TrimSpace(body.RunID)
	if kbID == "" || runID == "" || len(runID) > 64 {
		c.Error(errors.NewValidationError("kb_id and run_id are required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleAdmin)
	if !ok {
		return
	}
	undoer, ok := h.memorySvc.(interfaces.MemoryV2DreamUndoer)
	if !ok {
		c.Error(errors.NewServiceUnavailableError("Undo is not available"))
		return
	}
	result, err := undoer.UndoDreamRun(ctx, tenantID, kbID, runID)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to undo the Dreamer pass", tenantID, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
