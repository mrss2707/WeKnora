package container

import (
	"strings"
	"testing"

	"go.uber.org/dig"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// stubMemoryV2Deps provides the minimal outside types the Memory V2 subgraph
// needs (DB handle, model service, config, event manager). gorm.DB is never
// touched by the constructors used here, so a zero value is safe.
func stubMemoryV2Deps(c *dig.Container) {
	must(c.Provide(func() *gorm.DB { return &gorm.DB{} }))
	must(c.Provide(func() interfaces.ModelService { return nil }))
	must(c.Provide(func() *config.Config { return &config.Config{} }))
	must(c.Provide(chatpipeline.NewEventManager))
	must(c.Provide(NewResourceCleaner, dig.As(new(interfaces.ResourceCleaner))))
}

// TestRegisterMemoryV2WiresFullGraph resolves every type the module
// registers: repository interface, service interface and handler. It also
// proves dig.As(new(interfaces.MemoryRepositoryV2)) is preserved.
func TestRegisterMemoryV2WiresFullGraph(t *testing.T) {
	c := dig.New()
	stubMemoryV2Deps(c)
	if err := registerMemoryV2(c); err != nil {
		t.Fatalf("registerMemoryV2: %v", err)
	}

	err := c.Invoke(func(
		repo interfaces.MemoryRepositoryV2,
		svc interfaces.MemoryServiceV2,
		h *handler.MemoryV2Handler,
	) {
		if repo == nil || svc == nil || h == nil {
			t.Fatalf("resolved nil from Memory V2 graph: repo=%v svc=%v handler=%v", repo, svc, h)
		}
	})
	if err != nil {
		t.Fatalf("resolve Memory V2 graph: %v", err)
	}
}

// TestRegisterMemoryV2TwiceFails proves registration is single-shot: dig must
// reject re-registering the same constructors, catching accidental duplicate
// wiring during merges.
func TestRegisterMemoryV2TwiceFails(t *testing.T) {
	c := dig.New()
	stubMemoryV2Deps(c)
	if err := registerMemoryV2(c); err != nil {
		t.Fatalf("first registerMemoryV2: %v", err)
	}
	if err := registerMemoryV2(c); err == nil {
		t.Fatal("second registerMemoryV2 succeeded, want duplicate-provider error")
	}
}

// TestMissingMemoryV2ProviderNamesIt proves the graph error names the missing
// type when a provider is dropped (the "remove one provider" check).
func TestMissingMemoryV2ProviderNamesIt(t *testing.T) {
	c := dig.New()
	stubMemoryV2Deps(c)
	// Intentionally do NOT call registerMemoryV2.
	err := c.Invoke(func(svc interfaces.MemoryServiceV2) {
		t.Fatal("unexpectedly resolved MemoryServiceV2 without registration")
	})
	if err == nil {
		t.Fatal("Invoke succeeded without Memory V2 providers, want error")
	}
	if !strings.Contains(err.Error(), "MemoryServiceV2") {
		t.Fatalf("error %q does not name the missing MemoryServiceV2 type", err)
	}
}

func TestMemoryV2PoolDelta(t *testing.T) {
	if got := MemoryV2PoolDelta(nil); got != 0 {
		t.Fatalf("nil cfg delta = %d, want 0", got)
	}
	if got := MemoryV2PoolDelta(&config.Config{}); got != 0 {
		t.Fatalf("unset MemoryV2 delta = %d, want 0", got)
	}
	if got := MemoryV2PoolDelta(&config.Config{MemoryV2: &types.MemoryV2Config{Enabled: false}}); got != 0 {
		t.Fatalf("disabled delta = %d, want 0", got)
	}
	if got := MemoryV2PoolDelta(&config.Config{MemoryV2: &types.MemoryV2Config{Enabled: true}}); got != 5 {
		t.Fatalf("enabled default delta = %d, want 5", got)
	}
	t.Setenv("MEMORY_V2_DB_POOL_DELTA", "12")
	if got := MemoryV2PoolDelta(&config.Config{MemoryV2: &types.MemoryV2Config{Enabled: true}}); got != 12 {
		t.Fatalf("env override delta = %d, want 12", got)
	}
	t.Setenv("MEMORY_V2_DB_POOL_DELTA", "garbage")
	if got := MemoryV2PoolDelta(&config.Config{MemoryV2: &types.MemoryV2Config{Enabled: true}}); got != 5 {
		t.Fatalf("invalid env delta = %d, want fallback 5", got)
	}
}
