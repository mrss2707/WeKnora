package handler

import (
	"context"
	stderrors "errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Memory V2 data lives in the tenant that OWNS the knowledge base, so every
// member of a shared KB sees the same memories. The caller's own tenant is only
// used for requests that name no knowledge base. user_id merely records the
// author and is never an access boundary.

// scopeForKB authorizes the caller for kbID at the required role and returns the
// context and tenant the memory data must be read from / written to. With no
// kbID (or no KB lookup wired) it keeps the tenant-wide, caller-tenant scope.
// On failure it records the error on c and reports ok=false.
func (h *MemoryV2Handler) scopeForKB(
	c *gin.Context, kbID string, required types.OrgMemberRole,
) (ctx context.Context, tenantID string, ok bool) {
	callerTenant, hasTenant := h.getTenantID(c)
	if !hasTenant {
		c.Error(apperrors.NewBadRequestError("Tenant ID cannot be empty"))
		return nil, "", false
	}
	kbID = strings.TrimSpace(kbID)
	if kbID == "" || h.kbService == nil {
		return c.Request.Context(), callerTenant, true
	}
	grant, err := resolveHandlerKBAccessFor(c, kbID, h.kbService, h.kbShares, nil, required)
	if err != nil {
		c.Error(err)
		return nil, "", false
	}
	return grant.Context(c.Request.Context()), strconv.FormatUint(grant.EffectiveTenantID, 10), true
}

// scopeForMemory resolves the scope of a memory addressed by ID alone. The
// memory's own knowledge base decides the tenant; the caller must hold the
// required role on that KB. Memories of a deleted or unset KB are only
// reachable from the tenant that stores them.
func (h *MemoryV2Handler) scopeForMemory(
	c *gin.Context, id string, required types.OrgMemberRole,
) (ctx context.Context, tenantID string, ok bool) {
	callerTenant, hasTenant := h.getTenantID(c)
	if !hasTenant {
		c.Error(apperrors.NewBadRequestError("Tenant ID cannot be empty"))
		return nil, "", false
	}
	locator, canLocate := h.memoryRepo.(interfaces.MemoryV2KBScopeRepository)
	if !canLocate || h.kbService == nil {
		return c.Request.Context(), callerTenant, true
	}
	reqCtx := c.Request.Context()
	mem, err := locator.FindByID(reqCtx, id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			c.Error(apperrors.NewNotFoundError("Memory not found"))
		} else {
			logger.ErrorWithFields(reqCtx, err, map[string]interface{}{"memory_id": id})
			c.Error(apperrors.NewInternalServerError("Failed to load memory"))
		}
		return nil, "", false
	}

	if mem.KbID != "" {
		_, kbErr := h.kbService.GetKnowledgeBaseByID(reqCtx, mem.KbID)
		switch {
		case kbErr == nil:
			scopeCtx, owner, authorized := h.scopeForKB(c, mem.KbID, required)
			if !authorized {
				return nil, "", false
			}
			// Rows not yet repaired may still sit in their writer's tenant.
			if mem.TenantID != owner && mem.TenantID != callerTenant {
				c.Error(apperrors.NewNotFoundError("Memory not found"))
				return nil, "", false
			}
			if mem.TenantID != owner {
				owner = mem.TenantID
			}
			return scopeCtx, owner, true
		case !stderrors.Is(kbErr, repository.ErrKnowledgeBaseNotFound):
			logger.ErrorWithFields(reqCtx, kbErr, map[string]interface{}{"kb_id": mem.KbID})
			c.Error(apperrors.NewInternalServerError("Failed to resolve knowledge base"))
			return nil, "", false
		}
	}
	if mem.TenantID != callerTenant {
		c.Error(apperrors.NewNotFoundError("Memory not found"))
		return nil, "", false
	}
	return reqCtx, callerTenant, true
}

// queryFirst returns the first non-empty trimmed query value among keys. The
// web UI historically sent `type`/`verdict`/`keyword` while the API documents
// `memory_type`/`verdicts`/`q`; both spellings are accepted.
func queryFirst(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(c.Query(k)); v != "" {
			return v
		}
	}
	return ""
}

// authorFilter maps `author=me` (or an explicit user id) to the repository's
// author filter. Empty means every member's memories.
func (h *MemoryV2Handler) authorFilter(c *gin.Context) string {
	author := strings.TrimSpace(c.Query("author"))
	if author == "me" {
		uid, _ := h.getUserID(c)
		return uid
	}
	return author
}

// requireTenant reports a missing tenant before any other validation so the
// error order matches the rest of the API.
func (h *MemoryV2Handler) requireTenant(c *gin.Context) bool {
	if _, ok := h.getTenantID(c); !ok {
		c.Error(apperrors.NewBadRequestError("Tenant ID cannot be empty"))
		return false
	}
	return true
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
