package container

import (
	"os"
	"strconv"

	"go.uber.org/dig"

	memoryRepoV2 "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	memoryServiceV2 "github.com/Tencent/WeKnora/internal/application/service/memory_v2"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

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
		return memoryServiceV2.NewMemoryServiceV2(repo, modelSvc, *memCfg, nil)
	}); err != nil {
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

	return container.Provide(handler.NewMemoryV2Handler)
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