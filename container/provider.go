package container

import (
	"context"

	credproxy "github.com/takezoh/credproxy/credproxy"
)

// Provider is implemented by each credential backend.
// Lifecycle: Init once at startup, Routes to register HTTP handlers, ContainerSpec per launch.
type Provider interface {
	Name() string
	Init() error
	Routes() []credproxy.Route
	ContainerSpec(ctx context.Context, projectPath string) (Spec, error)
}
