package workers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/telemetry"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GraphJobs retries graph derivation from stored, verified vectors. It never
// calls a model or changes memory content and is gated by WorkerMutations.
type GraphJobs struct {
	repo     interfaces.MemoryRepositoryV2
	queue    interfaces.MemoryV2GraphJobs
	linker   *AutoLinker
	workerID string
}

func NewGraphJobs(repo interfaces.MemoryRepositoryV2, linker *AutoLinker) *GraphJobs {
	queue, ok := repo.(interfaces.MemoryV2GraphJobs)
	if !ok || linker == nil {
		return nil
	}
	return &GraphJobs{repo: repo, queue: queue, linker: linker, workerID: "graph-" + uuid.NewString()}
}

func (w *GraphJobs) Run(ctx context.Context) {
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")
	for ctx.Err() == nil {
		job, err := w.queue.ClaimGraphLink(ctx, w.workerID, 2*time.Minute)
		if err != nil {
			logger.Warnf(ctx, "graph jobs: claim failed: %v", err)
		} else if job != nil {
			started := time.Now()
			pass, cancel := context.WithTimeout(ctx, time.Minute)
			count, err := w.process(pass, job)
			cancel()
			outcome := "completed"
			if err != nil {
				outcome = "failed"
				logger.Warnf(ctx, "graph job %s generation %d: %v", job.ID, job.Generation, err)
			}
			// Release on graceful cancellation too; a killed process is recovered by
			// lease expiry, and stale generations cannot acknowledge a newer job.
			ack, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			finishErr := w.queue.FinishGraphLink(ack, job, err == nil)
			stop()
			if finishErr != nil {
				outcome = "failed"
				logger.Warnf(ctx, "graph job %s completion: %v", job.ID, finishErr)
			}
			telemetry.WorkerRun("graph", outcome, started)
			telemetry.WorkerActions("graph", "applied", count)
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *GraphJobs) process(ctx context.Context, job *types.MemoryGraphJob) (int, error) {
	memory, err := w.repo.GetByID(ctx, job.TenantID, job.MemoryID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	} // deleted while queued
	if err != nil {
		return 0, err
	}
	if memory == nil {
		return 0, nil
	}
	if memory.KbID != job.KbID || memory.TenantID != job.TenantID {
		return 0, fmt.Errorf("graph job scope changed")
	}
	if isZeroVector(memory.Embedding.Slice()) || metadataEmbeddingModel(memory.Metadata) == "" {
		return 0, fmt.Errorf("graph job needs verified stored embedding; run Admin relink")
	}
	return w.linker.LinkMemoryWithError(ctx, memory)
}
