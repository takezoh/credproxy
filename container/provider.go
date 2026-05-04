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

// PeriodicRegistrar is an optional Provider extension for scheduling periodic background tasks.
// If a provider implements this interface, RegisterPeriodic is called once after the credproxy
// Server is created and before Init.
type PeriodicRegistrar interface {
	RegisterPeriodic(srv *credproxy.Server)
}
