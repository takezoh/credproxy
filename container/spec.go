package container

// Spec is the per-container contribution from a single credential provider.
// Env keys and Mounts are merged by the runner across all providers.
type Spec struct {
	Env    map[string]string
	Mounts []string
}
