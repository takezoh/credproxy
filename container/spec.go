package container

// BridgeSpec describes a TCP↔unix socket bridge to start inside the container.
// The bridge listens on ListenAddr (TCP) and forwards to ContainerSocketPath (unix).
// Used by providers that need an in-container TCP endpoint but communicate with the
// host via a bind-mounted unix socket (e.g. GCP metadata server emulation).
type BridgeSpec struct {
	// ListenAddr is the TCP address to listen on inside the container (e.g. "127.0.0.1:8181").
	ListenAddr string
	// ContainerSocketPath is the unix socket path accessible inside the container.
	ContainerSocketPath string
}

// Spec is the per-container contribution from a single credential provider.
// Env keys, Mounts, and BridgeSpecs are merged by the runner across all providers.
type Spec struct {
	Env         map[string]string
	Mounts      []string
	BridgeSpecs []BridgeSpec
}
