package handler

import (
	stderrors "errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// createMemoryRequest is the client-writable part of a new memory. Server-owned
// fields (id, tenant, author, verdict, scores, counters) are never accepted.
type createMemoryRequest struct {
	KbID      string `json:"kb_id"`
	Content   string `json:"content"`
	SessionID string `json:"session_id"`
}

// updateMemoryRequest is a partial update; see interfaces.MemoryV2Patch.
type updateMemoryRequest struct {
	interfaces.MemoryV2Patch
}

// ---------------------------------------------------------------------------
// CreateMemory
// POST /api/v1/memories
// ---------------------------------------------------------------------------

// CreateMemory creates a new memory via the ingestion pipeline. Responds 201
// when stored, 200 with created=false when an existing memory already covers
// the content.
func (h *MemoryV2Handler) CreateMemory(c *gin.Context) {
	var req createMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request: " + err.Error()))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, req.KbID, types.OrgRoleEditor)
	if !ok || !h.limitWrite(c) {
		return
	}

	memory := &types.AgentMemory{
		TenantID:  tenantID,
		KbID:      strings.TrimSpace(req.KbID),
		UserID:    types.HumanUserIDOrEmpty(ctx),
		Content:   req.Content,
		SessionID: strings.TrimSpace(req.SessionID),
	}
	result, err := h.memorySvc.SaveMemory(ctx, memory)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to save memory", tenantID, "")
		return
	}

	statusCode := http.StatusOK
	if result.Created {
		statusCode = http.StatusCreated
	}
	c.JSON(statusCode, gin.H{"success": true, "data": result})
}

// ---------------------------------------------------------------------------
// UpdateMemory
// PUT /api/v1/memories/:id
// ---------------------------------------------------------------------------

// UpdateMemory applies a partial update (content, verdict, importance, tier,
// memory_type, tags) and returns the updated memory. Send the memory's
// updated_at as expected_updated_at (or an If-Unmodified-Since header) to
// reject the write with 409 when someone else changed it meanwhile.
func (h *MemoryV2Handler) UpdateMemory(c *gin.Context) {
	if !h.requireTenant(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}

	var req updateMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request: " + err.Error()))
		return
	}
	if req.ExpectedUpdatedAt == nil {
		if raw := strings.TrimSpace(c.GetHeader("If-Unmodified-Since")); raw != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				req.ExpectedUpdatedAt = &t
			}
		}
	}
	if req.IsEmpty() {
		c.Error(errors.NewBadRequestError("No updatable fields provided"))
		return
	}

	ctx, tenantID, ok := h.scopeForMemory(c, id, types.OrgRoleEditor)
	if !ok || !h.limitWrite(c) {
		return
	}
	patcher, ok := h.memorySvc.(interfaces.MemoryV2Patcher)
	if !ok {
		c.Error(errors.NewServiceUnavailableError("Memory updates are not available"))
		return
	}
	memory, err := patcher.PatchMemory(ctx, tenantID, id, req.MemoryV2Patch)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to update memory", tenantID, id)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": memory})
}

// writeMemoryError maps Memory V2 service errors to API errors: validation →
// 400, missing → 404, protected verdict or conflict → 409, else → 500.
func (h *MemoryV2Handler) writeMemoryError(c *gin.Context, err error, msg, tenantID, memoryID string) {
	var validation *types.ErrMemoryValidation
	var protected *types.ErrProtectedVerdict
	var conflict *types.MemoryV2ConflictError
	switch {
	case stderrors.As(err, &conflict):
		appErr := errors.NewConflictError(conflict.Message)
		if conflict.ExistingID != "" {
			appErr.Details = gin.H{"existing_id": conflict.ExistingID}
		}
		c.Error(appErr)
	case stderrors.As(err, &validation):
		c.Error(errors.NewValidationError(validation.Message))
	case stderrors.Is(err, gorm.ErrRecordNotFound):
		c.Error(errors.NewNotFoundError("Memory not found"))
	case stderrors.As(err, &protected):
		c.Error(errors.NewConflictError(protected.Error()))
	default:
		logger.ErrorWithFields(c.Request.Context(), err, map[string]interface{}{
			"tenant_id": tenantID,
			"memory_id": memoryID,
		})
		c.Error(errors.NewInternalServerError(msg))
	}
}
