package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ interfaces.MemoryV2VersionedRelationWriter = (*MemoryRepository)(nil)

func (r *MemoryRepository) CreateRelationForVersions(ctx context.Context, relation *types.MemoryRelation, fromVersion, toVersion time.Time) (bool, error) {
	if relation == nil || relation.TenantID == "" || relation.FromUUID == relation.ToUUID {
		return false, fmt.Errorf("invalid relation endpoints")
	}
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []*types.AgentMemory
		// SHARE conflicts with soft-delete/content updates, but allows concurrent
		// graph reads. Endpoint locks remain held until the insert commits.
		if err := tx.Where("tenant_id = ? AND id IN ?", relation.TenantID, []string{relation.FromUUID, relation.ToUUID}).Order("id").Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 2 {
			return gorm.ErrRecordNotFound
		}
		endpoints := map[string]*types.AgentMemory{}
		for _, row := range rows {
			endpoints[row.ID] = row
		}
		from, to := endpoints[relation.FromUUID], endpoints[relation.ToUUID]
		if from.KbID != to.KbID {
			return fmt.Errorf("relation across knowledge bases is forbidden")
		}
		if !from.UpdatedAt.Truncate(time.Microsecond).Equal(fromVersion.Truncate(time.Microsecond)) || !to.UpdatedAt.Truncate(time.Microsecond).Equal(toVersion.Truncate(time.Microsecond)) {
			return &types.MemoryV2ConflictError{Message: "graph candidate changed; retry with current vectors"}
		}
		if relation.ID == "" {
			relation.ID = uuid.NewString()
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(relation)
		created = result.RowsAffected == 1
		return result.Error
	})
	return created, err
}
