package container

import (
	"context"
	"os"
	"strconv"
	"time"

	"go.uber.org/dig"

	memoryRepoV2 "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	memoryServiceV2 "github.com/Tencent/WeKnora/internal/application/service/memory_v2"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type memoryV2MCPRegistrationParams struct {
	dig.In

	Server *mcpserver.Server `optional:"true"`
	Memory interfaces.ScopedMemoryV2Service
}

// registerMemoryV2 wires the Memory V2 module (repository, lazy service, chat
// pipeline plugin, HTTP handler) into the DI container. Memory V2 is the
// default runtime, so it is registered unconditionally. container.go keeps a
// single stable call to this function so upstream merges only ever touch one
// line instead of four scattered registration blocks.
//
// The memory_service v2 provider keeps its lazy model/embedder resolution and
// the types.DefaultMemoryV2Config fallback; no provider name or type changes.
func registerMemoryV2(container *dig.Container) error {
	if err := container.Provide(memoryRepoV2.NewMemoryRepository, dig.As(new(interfaces.MemoryRepositoryV2))); err != nil {
		return err
	}

	// Memory V2 service — embedder and chat model are resolved lazily at first
	// use to avoid tenant context dependency during DI registration.
	if err := container.Provide(func(
		repo interfaces.MemoryRepositoryV2,
		modelSvc interfaces.ModelService,
		cfg *config.Config,
	) interfaces.MemoryServiceV2 {
		memCfg := cfg.MemoryV2
		if memCfg == nil {
			defaults := types.DefaultMemoryV2Config()
			memCfg = &defaults
		}
		var svc *memoryServiceV2.MemoryServiceV2Impl = memoryServiceV2.NewMemoryServiceV2(repo, modelSvc, *memCfg, nil)
		// Lite mode (SQLite) cannot host the pgvector-backed memory tables;
		// mark the module not ready with the concrete reason so workers stay
		// down and every entry point rejects work instead of hitting SQLite.
		if os.Getenv("DB_DRIVER") == "sqlite" {
			svc.SetReadinessReason(types.MemoryV2ReasonLiteMode)
			logger.Warnf(context.Background(), "[MemoryV2] disabled: %s", types.MemoryV2ReasonLiteMode)
		}
		return svc
	}); err != nil {
		return err
	}

	if err := container.Provide(memoryServiceV2.NewScopedMemoryV2Service); err != nil {
		return err
	}

	if err := container.Invoke(func(
		eventManager *chatpipeline.EventManager,
		memV2 interfaces.MemoryServiceV2,
	) {
		chatpipeline.NewMemoryPluginV2(eventManager, memV2)
	}); err != nil {
		return err
	}

	// Shutdown hook: register the worker cleanup so app teardown stops the V2
	// workers exactly once, via the shared ResourceCleaner.
	if err := container.Invoke(func(
		cleaner interfaces.ResourceCleaner,
		memV2 interfaces.MemoryServiceV2,
	) {
		cleaner.RegisterWithName("MemoryV2", func() error {
			memV2.Cleanup()
			return nil
		})
	}); err != nil {
		return err
	}

	if err := container.Invoke(func(memV2 interfaces.MemoryServiceV2) {
		if readiness := memV2.Readiness(); !readiness.Ready {
			logger.Warnf(context.Background(), "[MemoryV2] not ready: %s", readiness.Reason)
		}
	}); err != nil {
		return err
	}

	if err := container.Invoke(func(memV2 interfaces.MemoryServiceV2, repo interfaces.MemoryRepositoryV2) {
		if !memV2.Readiness().Ready {
			return
		}
		go runMemoryV2KBScopeRepair(repo)
	}); err != nil {
		return err
	}

	if err := container.Invoke(func(params memoryV2MCPRegistrationParams) error {
		if params.Server == nil {
			return nil
		}
		return mcpserver.RegisterMemoryV2Tools(params.Server, params.Memory)
	}); err != nil {
		return err
	}

	return container.Provide(handler.NewMemoryV2Handler)
}

// runMemoryV2KBScopeRepair detects memories stored under a tenant other than
// their knowledge base owner's (written through shared KBs before memories were
// owner-scoped) and migrates them. Failures are logged, never fatal.
func runMemoryV2KBScopeRepair(repo interfaces.MemoryRepositoryV2) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[MemoryV2] kb scope repair panicked: %v", r)
		}
	}()
	mode := memoryServiceV2.KBScopeRepairModeFromEnv()
	report, err := memoryServiceV2.RepairKBTenantScope(ctx, repo, mode)
	if err != nil {
		logger.Errorf(ctx, "[MemoryV2] kb scope repair (%s) failed: %v", mode, err)
	}
	if report == nil || report.Detected == 0 {
		return
	}
	logger.Warnf(ctx,
		"[MemoryV2] kb scope repair mode=%s: detected=%d moved=%d failed=%d relations_retenanted=%d relations_dropped=%d moves=%v",
		mode, report.Detected, report.Moved, report.Failed, report.Relations, report.Dropped, report.Migrations)
}

// MemoryV2PoolDelta returns the extra connection-pool headroom granted to the
// shared GORM pool when Memory V2 is enabled (its workers compete with HTTP
// handlers for connections). GOVERNS by MEMORY_V2_DB_POOL_DELTA (default 5);
// returns 0 when Memory V2 is disabled or unconfigured.
func MemoryV2PoolDelta(cfg *config.Config) int {
	if cfg == nil || cfg.MemoryV2 == nil || !cfg.MemoryV2.Enabled {
		return 0
	}
	delta := 5
	if v := os.Getenv("MEMORY_V2_DB_POOL_DELTA"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			delta = d
		}
	}
	return delta
}
