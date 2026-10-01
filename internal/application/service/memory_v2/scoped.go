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

var errScopedMemoryNotFound = errors.New("memory was not found in the selected knowledge base")

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
	focal, err := s.Detail(ctx, scope, memoryID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > defaultScopedGraphLimit {
		limit = defaultScopedGraphLimit
	}
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
