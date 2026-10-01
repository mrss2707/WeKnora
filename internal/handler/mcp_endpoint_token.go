package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// RetrieveMCPEndpointToken returns the decrypted bearer token for an endpoint so
// the owner can auto-fill it in a config UI. It is the secret-equivalent of
// RotateToken and is guarded the same way (Admin). Legacy rows written before
// token auto-fill stored no ciphertext and come back with retrievable:false.
func (h *MCPEndpointHandler) RetrieveMCPEndpointToken(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	id := secutils.SanitizeForLog(c.Param("endpoint_id"))
	retriever, ok := h.svc.(interfaces.MCPEndpointTokenRetriever)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "token retrieval is not supported"})
		return
	}
	token, err := retriever.RetrieveToken(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, secutils.ErrEncryptedDataMissingKey) {
			// Ciphertext exists but SYSTEM_AES_KEY is missing/rotated: surface a
			// clear, actionable conflict rather than a generic 500.
			c.JSON(http.StatusConflict, gin.H{"error": "token is stored but cannot be decrypted; rotate the token to make it retrievable"})
			return
		}
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"token":       token,
		"retrievable": token != "",
	}})
}
