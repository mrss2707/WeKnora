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
	memorySvc  interfaces.MemoryServiceV2
	memoryRepo interfaces.MemoryRepositoryV2
}

// NewMemoryV2Handler creates a new MemoryV2Handler.
func NewMemoryV2Handler(
	memorySvc interfaces.MemoryServiceV2,
	memoryRepo interfaces.MemoryRepositoryV2,
) *MemoryV2Handler {
	return &MemoryV2Handler{
		memorySvc:  memorySvc,
		memoryRepo: memoryRepo,
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
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}

	filter := &types.MemoryFilter{
		TenantID:   tenantID,
		KbID:       strings.TrimSpace(c.Query("kb_id")),
		Limit:      pageSize,
		Offset:     (page - 1) * pageSize,
		MemoryType: strings.TrimSpace(c.Query("memory_type")),
		SessionID:  strings.TrimSpace(c.Query("session_id")),
	}

	if tierStr := strings.TrimSpace(c.Query("tier")); tierStr != "" {
		tier, err := strconv.Atoi(tierStr)
		if err == nil && tier >= 0 && tier <= 3 {
			filter.Tier = &tier
		}
	}

	if v := parseOptionalVerdicts(c.Query("verdicts")); len(v) > 0 {
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
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
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
// CreateMemory
// POST /api/v1/memories
// ---------------------------------------------------------------------------

// CreateMemory creates a new memory via the ingestion pipeline.
func (h *MemoryV2Handler) CreateMemory(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	var memory types.AgentMemory
	if err := c.ShouldBindJSON(&memory); err != nil {
		logger.Error(ctx, "Failed to parse create memory request", err)
		c.Error(errors.NewBadRequestError("Invalid request: " + err.Error()))
		return
	}

	memory.TenantID = tenantID
	if uid, ok := h.getUserID(c); ok {
		memory.UserID = uid
	}

	result, err := h.memorySvc.SaveMemory(ctx, &memory)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		c.Error(errors.NewInternalServerError("Failed to save memory: " + err.Error()))
		return
	}

	statusCode := http.StatusOK
	if result.Created {
		statusCode = http.StatusCreated
	}

	c.JSON(statusCode, gin.H{
		"success": true,
		"data":    result,
	})
}

// ---------------------------------------------------------------------------
// UpdateMemory
// PUT /api/v1/memories/:id
// ---------------------------------------------------------------------------

// UpdateMemory updates an existing memory.
func (h *MemoryV2Handler) UpdateMemory(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}

	var memory types.AgentMemory
	if err := c.ShouldBindJSON(&memory); err != nil {
		logger.Error(ctx, "Failed to parse update memory request", err)
		c.Error(errors.NewBadRequestError("Invalid request: " + err.Error()))
		return
	}

	memory.ID = id
	memory.TenantID = tenantID
	if uid, ok := h.getUserID(c); ok {
		memory.UserID = uid
	}

	result, err := h.memorySvc.SaveMemory(ctx, &memory)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id":  tenantID,
			"memory_id":  id,
		})
		c.Error(errors.NewInternalServerError("Failed to update memory: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// ---------------------------------------------------------------------------
// DeleteMemory
// DELETE /api/v1/memories/:id
// ---------------------------------------------------------------------------

// DeleteMemory soft-deletes a memory.
func (h *MemoryV2Handler) DeleteMemory(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}

	if err := h.memoryRepo.Delete(ctx, tenantID, id); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"memory_id": id,
		})
		c.Error(errors.NewInternalServerError("Failed to delete memory: " + err.Error()))
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
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.Error(errors.NewValidationError("query parameter 'q' is required"))
		return
	}

	filter := &types.MemoryFilter{
		TenantID:   tenantID,
		KbID:       strings.TrimSpace(c.Query("kb_id")),
		MemoryType: strings.TrimSpace(c.Query("memory_type")),
		SessionID:  strings.TrimSpace(c.Query("session_id")),
	}

	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err == nil && limit > 0 {
			filter.Limit = limit
		}
	}

	if v := parseOptionalVerdicts(c.Query("verdicts")); len(v) > 0 {
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
// GetMemoryGraph
// GET /api/v1/memories/graph/:id
// ---------------------------------------------------------------------------

// GetMemoryGraph returns a memory with its related nodes and edges.
func (h *MemoryV2Handler) GetMemoryGraph(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	id := c.Param("id")
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}

	// Get the focal memory
	memory, err := h.memoryRepo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"memory_id": id,
		})
		c.Error(errors.NewNotFoundError("Memory not found"))
		return
	}

	// Search for related memories using a content snippet as query
	queryLen := len(memory.Content)
	if queryLen > 200 {
		queryLen = 200
	}
	relatedFilter := &types.MemoryFilter{
		TenantID: tenantID,
		KbID:     strings.TrimSpace(c.Query("kb_id")),
		Limit:    20,
		Query:    memory.Content[:queryLen],
	}
	related, _, err := h.memoryRepo.Search(ctx, relatedFilter)
	if err != nil {
		// Non-fatal: return graph with just the focal node
		related = nil
	}

	// Build node list (de-duplicate by ID)
	seen := map[string]bool{id: true}
	nodes := []map[string]interface{}{
		{
			"id":        memory.ID,
			"content":   memory.Content,
			"type":      memory.MemoryType,
			"verdict":   memory.Verdict,
			"hub_score": memory.HubScore,
		},
	}
	edges := make([]map[string]interface{}, 0)

	for _, r := range related {
		if r.Memory == nil || r.Memory.ID == id {
			continue
		}
		if !seen[r.Memory.ID] {
			seen[r.Memory.ID] = true
			nodes = append(nodes, map[string]interface{}{
				"id":        r.Memory.ID,
				"content":   r.Memory.Content,
				"type":      r.Memory.MemoryType,
				"verdict":   r.Memory.Verdict,
				"hub_score": r.Memory.HubScore,
			})
		}
		edges = append(edges, map[string]interface{}{
			"source": id,
			"target": r.Memory.ID,
			"score":  r.Score,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"nodes": nodes,
			"edges": edges,
		},
	})
}

// ---------------------------------------------------------------------------
// GetMemoryStats
// GET /api/v1/memories/stats
// ---------------------------------------------------------------------------

// GetMemoryStats returns aggregate memory statistics for the tenant.
func (h *MemoryV2Handler) GetMemoryStats(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	// Total count (Limit=1 is enough since total is returned separately)
	totalFilter := &types.MemoryFilter{
		TenantID: tenantID,
		KbID:     strings.TrimSpace(c.Query("kb_id")),
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
		ft := &types.MemoryFilter{TenantID: tenantID, MemoryType: mt, Limit: 1}
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
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	issues, err := h.memorySvc.AssessHealth(ctx, tenantID, strings.TrimSpace(c.Query("kb_id")))
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
	ctx := c.Request.Context()

	tenantID, ok := h.getTenantID(c)
	if !ok {
		c.Error(errors.NewBadRequestError("Tenant ID cannot be empty"))
		return
	}

	result, err := h.memorySvc.ConsolidateDream(ctx, tenantID)
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
