package container

import (
	"fmt"

	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/config"
)

// registerCrossSessionMemory is the single gated runtime choice point for
// main's legacy cross-session memory (merge-isolation property 8). Memory V2
// is the default runtime and stays registered unconditionally via
// registerMemoryV2; the legacy service unlocks only when the
// `cross_session_memory` config flag is enabled, and it must be wired here
// exactly once — provider, chat recall plugin, routes and agent tools, all
// inventoried from main's container.go/router.go during the main merge.
//
// The legacy service package itself (internal/application/service/memory)
// arrives with the main merge. Until that merge lands, enabling the flag is a
// configuration error: this block fails closed instead of silently booting
// without the requested runtime. The merge resolution must fill the wiring
// inside the enabled branch and delete this fail-closed guard.
func registerCrossSessionMemory(container *dig.Container) error {
	var cfg *config.Config
	if err := container.Invoke(func(c *config.Config) { cfg = c }); err != nil {
		return fmt.Errorf("cross_session_memory: resolve config: %w", err)
	}

	// Gate closed (default): Memory V2 remains the only runtime.
	if cfg == nil || !cfg.CrossSessionMemory {
		return nil
	}

	// Merge integration point (Phase 11): register main's memory service and
	// its consumers exactly once here — e.g. Provide(NewMemoryService…),
	// recall plugin Invoke, routes and agent tools — instead of in
	// container.go/router.go.
	return fmt.Errorf(
		"cross_session_memory: flag is enabled but the legacy memory service wiring has not been integrated yet (lands with the main merge); refusing to boot without the requested runtime",
	)
}
