package container

import (
	"context"

	credproxy "github.com/takezoh/credproxy/credproxy"
)

// Provider is implemented by each credential backend.
// Lifecycle: Init once at startup, Routes to register HTTP handlers, ContainerSpec per
// launch, Materialize per launch after wiring is set up.
type Provider interface {
	Name() string
	Init() error
	Routes() []credproxy.Route
	// ContainerSpec returns the wiring (bind mounts, env, bridge specs) for a
	// container launch. It is a pure query: it MUST NOT perform credential-state
	// write side effects. Repeated calls with the same projectPath return
	// equivalent Spec values.
	ContainerSpec(ctx context.Context, projectPath string) (Spec, error)
	// Materialize prepares any host-side credential state that this provider owns
	// for projectPath. Idempotent: repeated calls with the same projectPath and no
	// external state change leave the observable filesystem state unchanged.
	// Returns nil on success, or a non-nil error describing the specific failure.
	// Providers with no host-side credential state implement this as a no-op
	// returning nil.
	//
	// Materialize does not retry internally (default retry = 0, fail fast).
	// Callers own the retry envelope and cadence.
	Materialize(ctx context.Context, projectPath string) error
}

// PeriodicRegistrar is an optional Provider extension for scheduling periodic background tasks.
// If a provider implements this interface, RegisterPeriodic is called once after the credproxy
// Server is created and before Init.
type PeriodicRegistrar interface {
	RegisterPeriodic(srv *credproxy.Server)
}
