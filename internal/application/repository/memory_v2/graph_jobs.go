package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ interfaces.MemoryV2GraphJobs = (*MemoryRepository)(nil)

func enqueueGraphLink(tx *gorm.DB, memory *types.AgentMemory) error {
	if memory == nil || memory.TenantID == "" || memory.ID == "" {
		return fmt.Errorf("invalid graph job scope")
	}
	if memory.KbID == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]interface{}{"memory_updated_at": memory.UpdatedAt})
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO extraction_queue
  (id,tenant_id,kb_id,memory_uuid,job_type,payload,generation,status,attempts,max_attempts,run_after)
  VALUES (?,?,?,?,'graph_link',?::jsonb,1,'pending',0,10,CURRENT_TIMESTAMP)
  ON CONFLICT (tenant_id,memory_uuid,job_type) WHERE job_type='graph_link'
  DO UPDATE SET kb_id=EXCLUDED.kb_id,payload=EXCLUDED.payload,generation=extraction_queue.generation+1,
    status='pending',attempts=0,max_attempts=10,run_after=CURRENT_TIMESTAMP,
    locked_by='',locked_until=NULL,error_message=NULL,updated_at=CURRENT_TIMESTAMP`,
		uuid.NewString(), memory.TenantID, memory.KbID, memory.ID, string(payload)).Error
}

func (r *MemoryRepository) EnqueueGraphLink(ctx context.Context, memory *types.AgentMemory) error {
	return enqueueGraphLink(r.db.WithContext(ctx), memory)
}

func (r *MemoryRepository) ClaimGraphLink(ctx context.Context, workerID string, ttl time.Duration) (*types.MemoryGraphJob, error) {
	if workerID == "" || ttl <= 0 {
		return nil, fmt.Errorf("invalid graph lease")
	}
	db := r.db.WithContext(ctx)
	// A crash during the last allowed attempt must not leave a permanent
	// processing row that neither succeeds nor becomes observable as failed.
	if err := db.Exec(`UPDATE extraction_queue SET status='failed',locked_by='',locked_until=NULL,
  error_message='graph lease expired after final attempt',updated_at=CURRENT_TIMESTAMP
  WHERE job_type='graph_link' AND status='processing' AND locked_until<CURRENT_TIMESTAMP AND attempts>=max_attempts`).Error; err != nil {
		return nil, err
	}
	var jobs []types.MemoryGraphJob
	err := db.Raw(`WITH picked AS (
  SELECT id FROM extraction_queue WHERE job_type='graph_link' AND attempts<max_attempts AND
    ((status='pending' AND run_after<=CURRENT_TIMESTAMP) OR (status='processing' AND locked_until<CURRENT_TIMESTAMP))
  ORDER BY run_after,id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE extraction_queue q SET status='processing',attempts=q.attempts+1,locked_by=?,
    locked_until=CURRENT_TIMESTAMP+make_interval(secs => ?),updated_at=CURRENT_TIMESTAMP
   FROM picked WHERE q.id=picked.id RETURNING q.*`, workerID, ttl.Seconds()).Scan(&jobs).Error
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

func (r *MemoryRepository) FinishGraphLink(ctx context.Context, job *types.MemoryGraphJob, success bool) error {
	if job == nil {
		return fmt.Errorf("nil graph job")
	}
	return r.db.WithContext(ctx).Exec(`UPDATE extraction_queue SET
   status=CASE WHEN ? THEN 'done' WHEN attempts>=max_attempts THEN 'failed' ELSE 'pending' END,
   run_after=CURRENT_TIMESTAMP+LEAST(60,POWER(2.0,attempts))*INTERVAL '1 second',
   locked_by='',locked_until=NULL,
   error_message=CASE WHEN ? THEN NULL ELSE 'graph linking failed; see server logs' END,
   updated_at=CURRENT_TIMESTAMP
  WHERE id=? AND tenant_id=? AND memory_uuid=? AND kb_id=? AND job_type='graph_link'
    AND status='processing' AND locked_by=? AND generation=?`,
		success, success, job.ID, job.TenantID, job.MemoryID, job.KbID, job.LockedBy, job.Generation).Error
}
