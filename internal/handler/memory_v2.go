package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MemoryV2Handler handles Memory V2 API requests (11 endpoints).
type MemoryV2Handler struct {
	memorySvc    interfaces.MemoryServiceV2
	memoryRepo   interfaces.MemoryRepositoryV2
	kbService    interfaces.KnowledgeBaseService
	kbShares     interfaces.KBShareService
	writeLimiter *memoryV2Limiter
	dreamLimiter *memoryV2Limiter
}

// NewMemoryV2Handler creates a new MemoryV2Handler.
func NewMemoryV2Handler(
	memorySvc interfaces.MemoryServiceV2,
	memoryRepo interfaces.MemoryRepositoryV2,
	kbService interfaces.KnowledgeBaseService,
	kbShares interfaces.KBShareService,
) *MemoryV2Handler {
	return &MemoryV2Handler{
		memorySvc:    memorySvc,
		memoryRepo:   memoryRepo,
		kbService:    kbService,
		kbShares:     kbShares,
		writeLimiter: memoryV2WriteLimiterFromEnv(),
		dreamLimiter: memoryV2DreamLimiterFromEnv(),
	}
}

// getTenantID extracts the tenant ID string from the auth context.
func (h *MemoryV2Handler) getTenantID(c *gin.Context) (string, bool) {
	tid := c.GetUint64(types.TenantIDContextKey.String())
	if tid == 0 {
		return "", false
	}
	return strconv.FormatUint(tid, 10), true
}

// getUserID extracts the user ID string from the auth context.
func (h *MemoryV2Handler) getUserID(c *gin.Context) (string, bool) {
	uid, exists := c.Get(types.UserIDContextKey.String())
	if !exists {
		return "", false
	}
	uidStr, ok := uid.(string)
	return uidStr, ok
}

// parseOptionalVerdicts parses a comma-separated verdicts query parameter.
func parseOptionalVerdicts(raw string) []types.MemoryVerdict {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]types.MemoryVerdict, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, types.MemoryVerdict(p))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// ListMemories
// GET /api/v1/memories
// ---------------------------------------------------------------------------

// ListMemories returns a paginated list of memories.
func (h *MemoryV2Handler) ListMemories(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}

	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}

	filter := &types.MemoryFilter{
		TenantID:     tenantID,
		KbID:         kbID,
		Limit:        pageSize,
		Offset:       (page - 1) * pageSize,
		MemoryType:   queryFirst(c, "memory_type", "type"),
		SessionID:    strings.TrimSpace(c.Query("session_id")),
		Query:        queryFirst(c, "keyword"),
		AuthorUserID: h.authorFilter(c),
	}

	if tierStr := strings.TrimSpace(c.Query("tier")); tierStr != "" {
		tier, err := strconv.Atoi(tierStr)
		if err == nil && tier >= 0 && tier <= 3 {
			filter.Tier = &tier
		}
	}

	if v := parseOptionalVerdicts(queryFirst(c, "verdicts", "verdict")); len(v) > 0 {
		filter.Verdicts = v
	}

	results, total, err := h.memoryRepo.Search(ctx, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"page":      page,
		})
		c.Error(errors.NewInternalServerError("Failed to list memories: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     results,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ---------------------------------------------------------------------------
// GetMemory
// GET /api/v1/memories/:id
// ---------------------------------------------------------------------------

// GetMemory retrieves a single memory by ID.
func (h *MemoryV2Handler) GetMemory(c *gin.Context) {
	if !h.requireTenant(c) {
		return
	}
	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForMemory(c, id, types.OrgRoleViewer)
	if !ok {
		return
	}

	memory, err := h.memoryRepo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"memory_id": id,
		})
		c.Error(errors.NewNotFoundError("Memory not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    memory,
	})
}

// ---------------------------------------------------------------------------
// DeleteMemory
// DELETE /api/v1/memories/:id
// ---------------------------------------------------------------------------

// DeleteMemory soft-deletes a memory.
func (h *MemoryV2Handler) DeleteMemory(c *gin.Context) {
	if !h.requireTenant(c) {
		return
	}
	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForMemory(c, id, types.OrgRoleEditor)
	if !ok || !h.limitWrite(c) {
		return
	}

	var err error
	if deleter, ok := h.memorySvc.(interfaces.MemoryV2Deleter); ok {
		err = deleter.DeleteMemory(ctx, tenantID, id)
	} else {
		err = h.memoryRepo.Delete(ctx, tenantID, id)
	}
	if err != nil {
		h.writeMemoryError(c, err, "Failed to delete memory", tenantID, id)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// ---------------------------------------------------------------------------
// SearchMemories
// GET /api/v1/memories/search
// ---------------------------------------------------------------------------

// SearchMemories performs the hybrid search pipeline.
func (h *MemoryV2Handler) SearchMemories(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.Error(errors.NewValidationError("query parameter 'q' is required"))
		return
	}

	kbID := strings.TrimSpace(c.Query("kb_id"))
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}

	filter := &types.MemoryFilter{
		TenantID:     tenantID,
		KbID:         kbID,
		MemoryType:   queryFirst(c, "memory_type", "type"),
		SessionID:    strings.TrimSpace(c.Query("session_id")),
		AuthorUserID: h.authorFilter(c),
	}

	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err == nil && limit > 0 {
			filter.Limit = limit
		}
	}

	if v := parseOptionalVerdicts(queryFirst(c, "verdicts", "verdict")); len(v) > 0 {
		filter.Verdicts = v
	}

	if ms := strings.TrimSpace(c.Query("min_score")); ms != "" {
		if v, err := strconv.ParseFloat(ms, 64); err == nil {
			filter.MinScore = v
		}
	}

	results, err := h.memorySvc.SearchMemories(ctx, query, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"query":     query,
		})
		c.Error(errors.NewInternalServerError("Failed to search memories: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    results,
	})
}

// ---------------------------------------------------------------------------
// GetMemoryStats
// GET /api/v1/memories/stats
// ---------------------------------------------------------------------------

// GetMemoryStats returns aggregate memory statistics for the tenant.
func (h *MemoryV2Handler) GetMemoryStats(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}

	if reader, ok := h.memoryRepo.(interfaces.MemoryV2StatsReader); ok {
		stats, err := reader.Stats(ctx, tenantID, kbID)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{"tenant_id": tenantID})
			c.Error(errors.NewInternalServerError("Failed to get memory stats"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
		return
	}

	// Total count (Limit=1 is enough since total is returned separately)
	totalFilter := &types.MemoryFilter{
		TenantID: tenantID,
		KbID:     kbID,
		Limit:    1,
	}
	_, total, err := h.memoryRepo.Search(ctx, totalFilter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError("Failed to get memory stats: " + err.Error()))
		return
	}

	// Count by memory type
	typeCounts := map[string]int64{}
	for _, mt := range []string{"semantic", "episodic", "procedural"} {
		ft := &types.MemoryFilter{TenantID: tenantID, KbID: kbID, MemoryType: mt, Limit: 1}
		_, cnt, _ := h.memoryRepo.Search(ctx, ft)
		if cnt > 0 {
			typeCounts[mt] = cnt
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total_memories": total,
			"by_type":        typeCounts,
		},
	})
}

// ---------------------------------------------------------------------------
// GetHealthReport
// GET /api/v1/memories/health
// ---------------------------------------------------------------------------

// GetHealthReport runs the 6 health checks and returns a HealthReport.
func (h *MemoryV2Handler) GetHealthReport(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}

	issues, err := h.memorySvc.AssessHealth(ctx, tenantID, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError("Failed to assess health: " + err.Error()))
		return
	}

	bySeverity := make(map[string]int)
	for _, issue := range issues {
		bySeverity[issue.Severity]++
	}

	report := &types.HealthReport{
		TenantID:    tenantID,
		CheckedAt:   time.Now(),
		TotalIssues: len(issues),
		BySeverity:  bySeverity,
		Issues:      issues,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    report,
	})
}

// ---------------------------------------------------------------------------
// TriggerDream
// POST /api/v1/memories/dream
// ---------------------------------------------------------------------------

// TriggerDream triggers one dreamer consolidation pass.
func (h *MemoryV2Handler) TriggerDream(c *gin.Context) {
	// A dream pass consolidates a whole tenant, so a shared KB's pass needs
	// admin rights on that KB. The KB may arrive in the query or JSON body.
	kbID := strings.TrimSpace(c.Query("kb_id"))
	dryRun := c.Query("dry_run") == "true"
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		var body struct {
			KbID   string `json:"kb_id"`
			DryRun bool   `json:"dry_run"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			if kbID == "" {
				kbID = strings.TrimSpace(body.KbID)
			}
			dryRun = dryRun || body.DryRun
		}
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleAdmin)
	if !ok || !h.limitDream(c, tenantID, kbID) {
		return
	}

	var result *types.DreamResult
	var err error
	previewer, canPreview := h.memorySvc.(interfaces.MemoryV2DreamPreviewer)
	if dryRun && (!canPreview || kbID == "") {
		c.Error(errors.NewBadRequestError("dry_run requires kb_id"))
		return
	}
	if dryRun {
		result, err = previewer.PreviewDreamScope(ctx, tenantID, kbID)
	} else if scoped, ok := h.memorySvc.(interfaces.MemoryV2ScopedDreamer); ok && kbID != "" {
		result, err = scoped.ConsolidateDreamScope(ctx, tenantID, kbID)
	} else {
		result, err = h.memorySvc.ConsolidateDream(ctx, tenantID)
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError("Failed to trigger dream: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// ---------------------------------------------------------------------------
// MemoryStatus
// GET /api/v1/tenants/memory-status
// ---------------------------------------------------------------------------

// readinessStatus maps a readiness reason string to the coarse status field
// reported by MemoryStatus: enabled, disabled (config/Lite) or not_ready.
func readinessStatus(reason string) string {
	switch {
	case reason == "" || reason == types.MemoryV2ReasonEnabled:
		return "enabled"
	case strings.HasPrefix(reason, "disabled"):
		return "disabled"
	default:
		return "not_ready"
	}
}

// MemoryStatus returns the memory backend status. Service readiness (config
// flag, Lite mode, repository wiring) is the first authority; the legacy
// repository probe only runs for a ready module.
func (h *MemoryV2Handler) MemoryStatus(c *gin.Context) {
	ctx := c.Request.Context()

	if h.memorySvc != nil {
		if r := h.memorySvc.Readiness(); !r.Ready {
			c.JSON(http.StatusOK, gin.H{
				"backend":   "v2",
				"available": false,
				"status":    readinessStatus(r.Reason),
				"reason":    r.Reason,
			})
			return
		}
	}

	if h.memoryRepo == nil {
		c.JSON(http.StatusOK, gin.H{
			"backend":   "v2",
			"available": false,
			"status":    "not_ready",
			"reason":    types.MemoryV2ReasonRepoUnavailable,
		})
		return
	}

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"backend":   "v2",
			"available": true,
			"status":    "enabled",
		})
		return
	}

	filter := &types.MemoryFilter{
		TenantID: tenantID,
		Limit:    1,
	}
	_, total, err := h.memoryRepo.Search(ctx, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		c.JSON(http.StatusOK, gin.H{
			"backend":   "v2",
			"available": true,
			"status":    "enabled",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"backend":      "v2",
		"available":    true,
		"memory_count": total,
		"status":       "enabled",
	})
}
