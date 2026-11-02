//go:build windows

package config_test

import (
	"testing"

	"github.com/takezoh/credproxy/cmd/credproxyd/config"
	"github.com/takezoh/credproxy/internal/nativeipc"
)

func TestLoadDefaultsToSIDScopedBrokerPipe(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	want, err := nativeipc.DefaultPipePath("broker")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPipe != want {
		t.Fatalf("listen_pipe = %q, want %q", cfg.ListenPipe, want)
	}
}
