package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// memoryHistoryItem is one audit entry as returned to clients. Before/after
// images carry content, so they are only returned to callers who can read
// the knowledge base (enforced by scopeForKB / scopeForMemory).
type memoryHistoryItem struct {
	*types.MemoryAuditEntry
	Before  json.RawMessage `json:"before,omitempty"`
	After   json.RawMessage `json:"after,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories/history?kb_id=&memory_id=&action=&page=&page_size=
// ---------------------------------------------------------------------------

// GetMemoryHistory lists the audit trail of a knowledge base (or one memory).
func (h *MemoryV2Handler) GetMemoryHistory(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	memoryID := strings.TrimSpace(c.Query("memory_id"))
	if kbID == "" && memoryID == "" {
		c.Error(errors.NewValidationError("kb_id or memory_id is required"))
		return
	}
	var (
		ctx      = c.Request.Context()
		tenantID string
		ok       bool
	)
	if memoryID != "" {
		ctx, tenantID, ok = h.scopeForMemory(c, memoryID, types.OrgRoleViewer)
	} else {
		ctx, tenantID, ok = h.scopeForKB(c, kbID, types.OrgRoleViewer)
	}
	if !ok {
		return
	}
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	reader, ok := h.memorySvc.(interfaces.MemoryV2HistoryReader)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": []any{}, "total": 0, "page": page, "page_size": pageSize}})
		return
	}
	filter := types.MemoryAuditFilter{
		TenantID: tenantID, KbID: kbID, MemoryID: memoryID,
		Limit: pageSize, Offset: (page - 1) * pageSize,
	}
	if a := strings.TrimSpace(c.Query("action")); a != "" {
		filter.Actions = strings.Split(a, ",")
	}
	entries, total, err := reader.History(ctx, filter)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to load memory history", tenantID, memoryID)
		return
	}
	items := make([]memoryHistoryItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, memoryHistoryItem{
			MemoryAuditEntry: e,
			Before:           rawOrNil(e.Before),
			After:            rawOrNil(e.After),
			Details:          rawOrNil(e.Details),
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": items, "total": total, "page": page, "page_size": pageSize,
	}})
}

func rawOrNil(b []byte) json.RawMessage {
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return json.RawMessage(b)
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories/:id/undo-merge
// ---------------------------------------------------------------------------

// UndoMemoryMerge reverses the latest merge into a memory (Admin on the KB).
func (h *MemoryV2Handler) UndoMemoryMerge(c *gin.Context) {
	if !h.requireTenant(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForMemory(c, id, types.OrgRoleAdmin)
	if !ok {
		return
	}
	undoer, ok := h.memorySvc.(interfaces.MemoryV2Undoer)
	if !ok {
		c.Error(errors.NewServiceUnavailableError("Undo is not available"))
		return
	}
	current, err := h.memoryRepo.GetByID(ctx, tenantID, id)
	if err != nil || current == nil {
		c.Error(errors.NewNotFoundError("Memory not found"))
		return
	}
	restored, err := undoer.UndoMerge(ctx, tenantID, current.KbID, id)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to undo merge", tenantID, id)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": restored})
}
