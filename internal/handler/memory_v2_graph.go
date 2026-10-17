package handler

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	memoryServiceV2 "github.com/Tencent/WeKnora/internal/application/service/memory_v2"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// graphLabelRunes bounds node labels (full content is fetched on demand).
const graphLabelRunes = 80

// memoryGraphNode / memoryGraphEdge are the wire format of the memory graph,
// matching the web UI's GraphNode / GraphEdge.
type memoryGraphNode struct {
	ID         string              `json:"id"`
	Label      string              `json:"label"`
	Type       string              `json:"type"`
	Importance int                 `json:"importance"`
	Verdict    types.MemoryVerdict `json:"verdict"`
	HubScore   float64             `json:"hub_score"`
	IsStale    bool                `json:"is_stale"`
	Focal      bool                `json:"focal,omitempty"`
}

type memoryGraphEdge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
}

type memoryGraphResponse struct {
	Nodes     []memoryGraphNode `json:"nodes"`
	Edges     []memoryGraphEdge `json:"edges"`
	Truncated bool              `json:"truncated"`
}

func graphLabel(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(content) <= graphLabelRunes {
		return content
	}
	return string([]rune(content)[:graphLabelRunes-1]) + "…"
}

func toGraphResponse(g *interfaces.MemoryV2Graph) memoryGraphResponse {
	out := memoryGraphResponse{Nodes: []memoryGraphNode{}, Edges: []memoryGraphEdge{}}
	if g == nil {
		return out
	}
	add := func(m *types.AgentMemory, focal bool) {
		if m == nil {
			return
		}
		stale := time.Since(m.CreatedAt) > 180*24*time.Hour && m.Importance < 1
		out.Nodes = append(out.Nodes, memoryGraphNode{
			ID: m.ID, Label: graphLabel(m.Content), Type: m.MemoryType, Importance: m.Importance,
			Verdict: m.Verdict, HubScore: m.HubScore, IsStale: stale, Focal: focal,
		})
	}
	add(g.Focal, true)
	for _, m := range g.Nodes {
		add(m, false)
	}
	for _, r := range g.Relations {
		if r == nil {
			continue
		}
		out.Edges = append(out.Edges, memoryGraphEdge{
			ID: r.ID, Source: r.FromUUID, Target: r.ToUUID, RelationType: r.RelationType, Weight: r.Weight,
		})
	}
	out.Truncated = g.Truncated
	return out
}

func (h *MemoryV2Handler) graphExplorer() interfaces.MemoryV2GraphExplorer {
	scoped, _ := memoryServiceV2.NewScopedMemoryV2Service(h.memorySvc, h.memoryRepo).(interfaces.MemoryV2GraphExplorer)
	return scoped
}

func queryInt(c *gin.Context, key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(c.Query(key))); err == nil {
		return v
	}
	return def
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories/graph/:id?depth=1..3&limit=
// ---------------------------------------------------------------------------

// GetMemoryGraph returns the relation graph around one memory, up to depth
// hops, inside the memory's knowledge base.
func (h *MemoryV2Handler) GetMemoryGraph(c *gin.Context) {
	if !h.requireTenant(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Memory ID is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForMemory(c, id, types.OrgRoleViewer)
	if !ok {
		return
	}
	memory, err := h.memoryRepo.GetByID(ctx, tenantID, id)
	if err != nil || memory == nil {
		c.Error(errors.NewNotFoundError("Memory not found"))
		return
	}
	g, err := h.graphExplorer().GraphDepth(ctx,
		interfaces.MemoryV2Scope{TenantID: tenantID, KnowledgeBaseID: memory.KbID}, id,
		queryInt(c, "depth", 1), queryInt(c, "limit", 100))
	if err != nil {
		h.writeGraphError(c, err, tenantID, id)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toGraphResponse(g)})
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories/graph?kb_id=&limit=
// ---------------------------------------------------------------------------

// GetMemoryGraphOverview returns the knowledge base's most connected memories
// and the relations among them.
func (h *MemoryV2Handler) GetMemoryGraphOverview(c *gin.Context) {
	kbID := strings.TrimSpace(c.Query("kb_id"))
	if kbID == "" {
		c.Error(errors.NewValidationError("kb_id is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleViewer)
	if !ok {
		return
	}
	g, err := h.graphExplorer().Overview(ctx,
		interfaces.MemoryV2Scope{TenantID: tenantID, KnowledgeBaseID: kbID}, queryInt(c, "limit", 100))
	if err != nil {
		h.writeGraphError(c, err, tenantID, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toGraphResponse(g)})
}

func (h *MemoryV2Handler) writeGraphError(c *gin.Context, err error, tenantID, id string) {
	if stderrors.Is(err, memoryServiceV2.ErrScopedMemoryNotFound) {
		c.Error(errors.NewNotFoundError("Memory not found"))
		return
	}
	h.writeMemoryError(c, err, "Failed to load memory graph", tenantID, id)
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories/relink {kb_id}
// ---------------------------------------------------------------------------

// RelinkMemories rebuilds the automatic relations of a knowledge base (Admin;
// shares the dream cooldown).
func (h *MemoryV2Handler) RelinkMemories(c *gin.Context) {
	var body struct {
		KbID string `json:"kb_id"`
	}
	_ = c.ShouldBindJSON(&body)
	kbID := strings.TrimSpace(firstNonBlank(c.Query("kb_id"), body.KbID))
	if kbID == "" {
		c.Error(errors.NewValidationError("kb_id is required"))
		return
	}
	ctx, tenantID, ok := h.scopeForKB(c, kbID, types.OrgRoleAdmin)
	if !ok || !h.limitDream(c, tenantID, "relink:"+kbID) {
		return
	}
	relinker, ok := h.memorySvc.(interfaces.MemoryV2Relinker)
	if !ok {
		c.Error(errors.NewServiceUnavailableError("Relink is not available"))
		return
	}
	res, err := relinker.RelinkKnowledgeBase(ctx, tenantID, kbID)
	if err != nil {
		h.writeMemoryError(c, err, "Failed to relink memories", tenantID, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}
