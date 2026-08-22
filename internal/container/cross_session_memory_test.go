package container

import (
	"strings"
	"testing"

	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/config"
)

// TestRegisterCrossSessionMemory_DefaultOff proves the gate is closed by
// default: the block is a no-op and BootContainer's must() cannot panic.
func TestRegisterCrossSessionMemory_DefaultOff(t *testing.T) {
	c := dig.New()
	must(c.Provide(func() *config.Config { return &config.Config{} }))
	if err := registerCrossSessionMemory(c); err != nil {
		t.Fatalf("flag off: %v", err)
	}
}

// TestRegisterCrossSessionMemory_NilConfigIsNoOp proves a nil config (never
// produced by LoadConfig, but defensive) keeps the gate closed.
func TestRegisterCrossSessionMemory_NilConfigIsNoOp(t *testing.T) {
	c := dig.New()
	must(c.Provide(func() *config.Config { return nil }))
	if err := registerCrossSessionMemory(c); err != nil {
		t.Fatalf("nil config: %v", err)
	}
}

// TestRegisterCrossSessionMemory_FlagOnFailsClosed proves enabling the flag
// before the main merge cannot silently boot without the legacy runtime.
func TestRegisterCrossSessionMemory_FlagOnFailsClosed(t *testing.T) {
	c := dig.New()
	must(c.Provide(func() *config.Config { return &config.Config{CrossSessionMemory: true} }))
	err := registerCrossSessionMemory(c)
	if err == nil {
		t.Fatal("flag on succeeded, want fail-closed error")
	}
	if !strings.Contains(err.Error(), "cross_session_memory") {
		t.Fatalf("error %q does not name the flag", err)
	}
}

// TestRegisterCrossSessionMemory_MissingConfigNamesTheType proves a dropped
// config provider surfaces as a wiring error naming the missing type.
func TestRegisterCrossSessionMemory_MissingConfigNamesTheType(t *testing.T) {
	c := dig.New()
	err := registerCrossSessionMemory(c)
	if err == nil {
		t.Fatal("Invoke succeeded without config provider, want error")
	}
	if !strings.Contains(err.Error(), "config.Config") {
		t.Fatalf("error %q does not name the missing config.Config type", err)
	}
}
