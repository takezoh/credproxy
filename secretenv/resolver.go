package secretenv

import (
	"context"
	"fmt"
)

// Hook resolves an opaque reference string to a real secret value.
// The reference format is determined by the backend (e.g. "op://vault/item/field").
// Backend authentication is the hook's own responsibility.
// Implementations must be safe for concurrent use.
type Hook interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

// Resolver resolves all entries from an env-file using a Hook.
// It is safe for concurrent use if the underlying Hook is.
type Resolver struct {
	hook Hook
}

// NewResolver creates a Resolver backed by hook.
func NewResolver(hook Hook) *Resolver {
	return &Resolver{hook: hook}
}

// ResolveFile parses the env-file at path and resolves every entry via the Hook.
// Returns a map of env var name → resolved secret value.
// All entries must resolve; a single failure aborts and returns an error.
func (r *Resolver) ResolveFile(ctx context.Context, path string) (map[string]string, error) {
	entries, err := ParseFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		val, err := r.hook.Resolve(ctx, e.Ref)
		if err != nil {
			return nil, fmt.Errorf("secretenv: resolve %s (%s): %w", e.Name, e.Ref, err)
		}
		if val == "" {
			return nil, fmt.Errorf("secretenv: resolve %s (%s): hook returned empty value", e.Name, e.Ref)
		}
		out[e.Name] = val
	}
	return out, nil
}
