package handler

import (
	"net/http"
	"sort"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler/dto"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// ModelPreferenceHandler handles model preference endpoints (default model
// selection and fallback ordering).
type ModelPreferenceHandler struct {
	repo interfaces.ModelRepository
}

// NewModelPreferenceHandler creates a new ModelPreferenceHandler.
func NewModelPreferenceHandler(
	repo interfaces.ModelRepository,
) *ModelPreferenceHandler {
	return &ModelPreferenceHandler{repo: repo}
}

// ListPreferences godoc
// @Summary      List model preferences
// @Description  Returns models of a given type sorted by sort_order ASC
// @Tags         Model Preferences
// @Accept       json
// @Produce      json
// @Param        type  query     string  true  "Model type (KnowledgeQA, Embedding, Rerank, VLLM, ASR)"
// @Success      200   {object}  map[string]interface{}
// @Failure      400   {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/preferences [get]
func (h *ModelPreferenceHandler) ListPreferences(c *gin.Context) {
	ctx := c.Request.Context()

	modelType := types.ModelType(c.Query("type"))
	if modelType == "" {
		logger.Error(ctx, "Model type is required")
		c.Error(errors.NewBadRequestError("Model type is required"))
		return
	}

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Tenant ID is empty")
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	models, err := h.repo.List(ctx, tenantID, modelType, "")
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"type":      modelType,
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].SortOrder < models[j].SortOrder
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewModelResponses(models),
	})
}

// setDefaultRequest is the body for SetDefault.
type setDefaultRequest struct {
	ModelID string          `json:"model_id" binding:"required"`
	Type    types.ModelType `json:"type"     binding:"required"`
}

// SetDefault godoc
// @Summary      Set default model
// @Description  Clears the current default for the type and marks the specified model as default
// @Tags         Model Preferences
// @Accept       json
// @Produce      json
// @Param        request  body  setDefaultRequest  true  "Model ID and type"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/preferences/default [put]
func (h *ModelPreferenceHandler) SetDefault(c *gin.Context) {
	ctx := c.Request.Context()

	var req setDefaultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Tenant ID is empty")
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	// Clear existing default for this type
	if err := h.repo.ClearDefaultByType(ctx, uint(tenantID), req.Type, ""); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"type":      req.Type,
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// Fetch the model and mark it as default
	model, err := h.repo.GetByID(ctx, tenantID, req.ModelID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  req.ModelID,
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if model == nil {
		c.Error(errors.NewNotFoundError("Model not found"))
		return
	}

	model.IsDefault = true
	if err := h.repo.Update(ctx, model); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  req.ModelID,
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Set default model: %s for type %s, tenant %d", req.ModelID, req.Type, tenantID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewModelResponse(model),
	})
}

// reorderRequest is the body for Reorder.
type reorderRequest struct {
	Type       types.ModelType `json:"type"        binding:"required"`
	OrderedIDs []string        `json:"ordered_ids" binding:"required"`
}

// Reorder godoc
// @Summary      Reorder models
// @Description  Persists the new sort_order for each model based on the provided ordered ID list
// @Tags         Model Preferences
// @Accept       json
// @Produce      json
// @Param        request  body  reorderRequest  true  "Model type and ordered IDs"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/preferences/reorder [put]
func (h *ModelPreferenceHandler) Reorder(c *gin.Context) {
	ctx := c.Request.Context()

	var req reorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Tenant ID is empty")
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	for i, id := range req.OrderedIDs {
		model, err := h.repo.GetByID(ctx, tenantID, id)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_id":  id,
				"tenant_id": tenantID,
			})
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
		if model == nil {
			logger.Errorf(ctx, "Model not found during reorder: %s", id)
			c.Error(errors.NewNotFoundError("Model not found: " + id))
			return
		}
		model.SortOrder = i
		if err := h.repo.Update(ctx, model); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_id":  id,
				"tenant_id": tenantID,
			})
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
	}

	logger.Infof(ctx, "Reordered models for type %s, tenant %d: %v", req.Type, tenantID, req.OrderedIDs)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}
