package memory_v2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const defaultScopedGraphLimit = 100

// ErrScopedMemoryNotFound is returned when a memory does not exist in the
// requested scope (it may exist elsewhere; callers must not reveal that).
var ErrScopedMemoryNotFound = errors.New("memory was not found in the selected knowledge base")

var errScopedMemoryNotFound = ErrScopedMemoryNotFound

// ScopedMemoryV2Service applies a trusted tenant/knowledge-base boundary around
// the broader Memory V2 service and repository contracts.
type ScopedMemoryV2Service struct {
	service interfaces.MemoryServiceV2
	repo    interfaces.MemoryRepositoryV2
}

var _ interfaces.ScopedMemoryV2Service = (*ScopedMemoryV2Service)(nil)

func NewScopedMemoryV2Service(
	service interfaces.MemoryServiceV2,
	repo interfaces.MemoryRepositoryV2,
) interfaces.ScopedMemoryV2Service {
	return &ScopedMemoryV2Service{service: service, repo: repo}
}

func (s *ScopedMemoryV2Service) Readiness() types.MemoryV2Readiness {
	if s == nil || s.service == nil {
		return types.MemoryV2Readiness{Ready: false, Reason: types.MemoryV2ReasonRepoUnavailable}
	}
	return s.service.Readiness()
}

func (s *ScopedMemoryV2Service) Recall(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	query string,
	limit int,
) ([]*types.MemorySearchResult, error) {
	if err := validateMemoryV2Scope(scope); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if ready := s.Readiness(); !ready.Ready {
		return nil, fmt.Errorf("memory V2 not ready: %s", ready.Reason)
	}
	results, err := s.service.SearchMemories(ctx, query, &types.MemoryFilter{
		TenantID: scope.TenantID,
		KbID:     scope.KnowledgeBaseID,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*types.MemorySearchResult, 0, len(results))
	for _, result := range results {
		if result == nil || result.Memory == nil {
			continue
		}
		if memoryInScope(result.Memory, scope) {
			out = append(out, result)
		}
	}
	return out, nil
}

func (s *ScopedMemoryV2Service) Save(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	content string,
	sessionID string,
) (*types.SaveMemoryResult, error) {
	if err := validateMemoryV2Scope(scope); err != nil {
		return nil, err
	}
	if ready := s.Readiness(); !ready.Ready {
		return nil, fmt.Errorf("memory V2 not ready: %s", ready.Reason)
	}
	result, err := s.service.SaveMemory(ctx, &types.AgentMemory{
		TenantID:  scope.TenantID,
		KbID:      scope.KnowledgeBaseID,
		UserID:    types.HumanUserIDOrEmpty(ctx),
		Content:   content,
		SessionID: strings.TrimSpace(sessionID),
	})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Memory == nil || !memoryInScope(result.Memory, scope) {
		return nil, errScopedMemoryNotFound
	}
	return result, nil
}

func (s *ScopedMemoryV2Service) Detail(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	memoryID string,
) (*types.AgentMemory, error) {
	if err := validateMemoryV2Scope(scope); err != nil {
		return nil, err
	}
	if strings.TrimSpace(memoryID) == "" {
		return nil, fmt.Errorf("memory_id is required")
	}
	if ready := s.Readiness(); !ready.Ready {
		return nil, fmt.Errorf("memory V2 not ready: %s", ready.Reason)
	}
	memory, err := s.repo.GetByID(ctx, scope.TenantID, strings.TrimSpace(memoryID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errScopedMemoryNotFound
		}
		return nil, fmt.Errorf("failed to load memory")
	}
	if !memoryInScope(memory, scope) {
		return nil, errScopedMemoryNotFound
	}
	return memory, nil
}

func (s *ScopedMemoryV2Service) Graph(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	memoryID string,
	limit int,
) (*interfaces.MemoryV2Graph, error) {
	return s.GraphDepth(ctx, scope, memoryID, 1, limit)
}

// Graph limits: depth is clamped to [1, maxGraphDepth]; limit bounds edges.
const (
	maxGraphDepth = 3
	maxGraphNodes = 300
)

var _ interfaces.MemoryV2GraphExplorer = (*ScopedMemoryV2Service)(nil)

// GraphDepth walks the relation graph breadth-first from one memory, up to
// depth hops, inside the scope. Every node is checked against the scope, so a
// relation can never pull in another tenant's or knowledge base's memory.
func (s *ScopedMemoryV2Service) GraphDepth(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	memoryID string,
	depth, limit int,
) (*interfaces.MemoryV2Graph, error) {
	focal, err := s.Detail(ctx, scope, memoryID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > defaultScopedGraphLimit {
		limit = defaultScopedGraphLimit
	}
	if depth < 1 {
		depth = 1
	}
	if depth > maxGraphDepth {
		depth = maxGraphDepth
	}
	store, batched := s.repo.(interfaces.MemoryV2GraphStore)
	if !batched {
		return s.graphOneHop(ctx, scope, focal, limit)
	}

	g := &interfaces.MemoryV2Graph{Focal: focal}
	nodes := map[string]*types.AgentMemory{focal.ID: focal}
	seenEdge := map[string]bool{}
	frontier := []string{focal.ID}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		rels, err := store.RelationsOf(ctx, scope.TenantID, frontier, limit+1)
		if err != nil {
			return nil, err
		}
		var unknown []string
		for _, r := range rels {
			for _, id := range []string{r.FromUUID, r.ToUUID} {
				if _, ok := nodes[id]; !ok && !contains(unknown, id) {
					unknown = append(unknown, id)
				}
			}
		}
		loaded, err := store.GetByIDs(ctx, scope.TenantID, unknown)
		if err != nil {
			return nil, err
		}
		inScope := map[string]*types.AgentMemory{}
		for _, m := range loaded {
			if memoryInScope(m, scope) {
				inScope[m.ID] = m
			}
		}
		var next []string
		for _, r := range rels {
			if seenEdge[r.ID] {
				continue
			}
			from, okF := nodes[r.FromUUID]
			if !okF {
				from, okF = inScope[r.FromUUID]
			}
			to, okT := nodes[r.ToUUID]
			if !okT {
				to, okT = inScope[r.ToUUID]
			}
			if !okF || !okT {
				continue // an endpoint is outside the scope
			}
			if len(g.Relations) >= limit || len(nodes) >= maxGraphNodes {
				g.Truncated = true
				break
			}
			seenEdge[r.ID] = true
			g.Relations = append(g.Relations, r)
			for _, m := range []*types.AgentMemory{from, to} {
				if _, ok := nodes[m.ID]; !ok {
					nodes[m.ID] = m
					g.Nodes = append(g.Nodes, m)
					next = append(next, m.ID)
				}
			}
		}
		if g.Truncated {
			break
		}
		frontier = next
	}
	return g, nil
}

// Overview returns the most connected memories of the knowledge base and the
// relations among them.
func (s *ScopedMemoryV2Service) Overview(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	limit int,
) (*interfaces.MemoryV2Graph, error) {
	if err := validateMemoryV2Scope(scope); err != nil {
		return nil, err
	}
	if ready := s.Readiness(); !ready.Ready {
		return nil, fmt.Errorf("memory V2 not ready: %s", ready.Reason)
	}
	store, ok := s.repo.(interfaces.MemoryV2GraphStore)
	if !ok {
		return &interfaces.MemoryV2Graph{}, nil
	}
	if limit <= 0 || limit > maxGraphNodes {
		limit = 100
	}
	hubs, err := store.TopHubs(ctx, scope.TenantID, scope.KnowledgeBaseID, limit)
	if err != nil {
		return nil, err
	}
	g := &interfaces.MemoryV2Graph{}
	ids := make([]string, 0, len(hubs))
	in := map[string]bool{}
	for _, m := range hubs {
		if memoryInScope(m, scope) {
			g.Nodes = append(g.Nodes, m)
			ids = append(ids, m.ID)
			in[m.ID] = true
		}
	}
	rels, err := store.RelationsOf(ctx, scope.TenantID, ids, limit*4)
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if in[r.FromUUID] && in[r.ToUUID] {
			g.Relations = append(g.Relations, r)
		}
	}
	return g, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// graphOneHop is the per-node fallback for repositories without batched
// graph loading.
func (s *ScopedMemoryV2Service) graphOneHop(
	ctx context.Context,
	scope interfaces.MemoryV2Scope,
	focal *types.AgentMemory,
	limit int,
) (*interfaces.MemoryV2Graph, error) {
	relations, err := s.repo.GetRelations(ctx, focal.ID, scope.TenantID)
	if err != nil {
		return nil, err
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Weight == relations[j].Weight {
			return relations[i].ID < relations[j].ID
		}
		return relations[i].Weight > relations[j].Weight
	})

	nodes := make([]*types.AgentMemory, 0, len(relations))
	validRelations := make([]*types.MemoryRelation, 0, len(relations))
	validNodes := map[string]*types.AgentMemory{focal.ID: focal}
	invalidNodes := map[string]struct{}{}
	truncated := false
	for _, relation := range relations {
		if relation == nil {
			continue
		}
		relatedID := relation.ToUUID
		if relatedID == focal.ID {
			relatedID = relation.FromUUID
		} else if relation.FromUUID != focal.ID {
			continue
		}
		if _, invalid := invalidNodes[relatedID]; invalid {
			continue
		}
		related, loaded := validNodes[relatedID]
		if !loaded {
			var err error
			related, err = s.repo.GetByID(ctx, scope.TenantID, relatedID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					invalidNodes[relatedID] = struct{}{}
					continue
				}
				return nil, fmt.Errorf("failed to load related memory")
			}
			if !memoryInScope(related, scope) {
				invalidNodes[relatedID] = struct{}{}
				continue
			}
		}
		if len(validRelations) >= limit {
			truncated = true
			break
		}
		if !loaded {
			validNodes[relatedID] = related
			nodes = append(nodes, related)
		}
		validRelations = append(validRelations, relation)
	}
	return &interfaces.MemoryV2Graph{
		Focal:     focal,
		Nodes:     nodes,
		Relations: validRelations,
		Truncated: truncated,
	}, nil
}

func (s *ScopedMemoryV2Service) Status(ctx context.Context, tenantID string) types.MemoryStatusResponse {
	readiness := s.Readiness()
	status := "not_ready"
	if readiness.Ready {
		status = "enabled"
	} else if strings.HasPrefix(readiness.Reason, "disabled:") {
		status = "disabled"
	}
	response := types.MemoryStatusResponse{
		Backend:   "v2",
		Available: readiness.Ready,
		Status:    status,
		Reason:    readiness.Reason,
	}
	if !readiness.Ready || s.repo == nil || strings.TrimSpace(tenantID) == "" {
		return response
	}
	_, total, err := s.repo.Search(ctx, &types.MemoryFilter{TenantID: tenantID, Limit: 1})
	if err != nil {
		response.Available = false
		response.Status = "not_ready"
		response.Reason = "not_ready: repository query failed"
		return response
	}
	response.MemoryCount = total
	response.Reason = ""
	return response
}

func validateMemoryV2Scope(scope interfaces.MemoryV2Scope) error {
	if strings.TrimSpace(scope.TenantID) == "" {
		return fmt.Errorf("memory tenant scope is required")
	}
	if strings.TrimSpace(scope.KnowledgeBaseID) == "" {
		return fmt.Errorf("memory knowledge base scope is required")
	}
	return nil
}

func memoryInScope(memory *types.AgentMemory, scope interfaces.MemoryV2Scope) bool {
	return memory != nil && memory.TenantID == scope.TenantID && memory.KbID == scope.KnowledgeBaseID
}
