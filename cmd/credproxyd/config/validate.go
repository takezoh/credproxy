package config

import (
	"fmt"
	"path/filepath"
	"time"
)

// validate checks the expanded Config for required fields.
func validate(c Config) error {
	if c.ListenTCP == "" && c.ListenUnix == "" {
		return fmt.Errorf("config: at least one of listen_tcp or listen_unix must be set")
	}
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("config: route[%d]: path is required", i)
		}
		if r.Upstream == "" && len(r.CredentialCommand) == 0 {
			return fmt.Errorf("config: route[%d]: upstream or credential_command is required", i)
		}
	}
	return validateOperations(c)
}

func validateOperations(c Config) error {
	if len(c.Operations) > 0 && c.DaemonRevision == "" {
		return fmt.Errorf("config: daemon_revision is required when operations are configured")
	}
	seen := make(map[string]bool)
	for i, op := range c.Operations {
		if op.Name == "" || seen[op.Name] {
			return fmt.Errorf("config: operation[%d]: unique name is required", i)
		}
		seen[op.Name] = true
		if op.BindingRevision == "" || op.Subcommand == "" || len(op.ExecutablePaths) == 0 || len(op.CredentialCommand) == 0 || len(op.Environment) == 0 {
			return fmt.Errorf("config: operation[%d]: binding, executable, subcommand, credential command, and env are required", i)
		}
		for _, path := range op.ExecutablePaths {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return fmt.Errorf("config: operation[%d]: executable paths must be clean absolute paths", i)
			}
		}
		providerPath := op.CredentialCommand[0]
		if !filepath.IsAbs(providerPath) || filepath.Clean(providerPath) != providerPath {
			return fmt.Errorf("config: operation[%d]: credential command must start with a clean absolute path", i)
		}
		argSeen := make(map[string]bool)
		for _, arg := range op.Arguments {
			if arg.Flag == "" || argSeen[arg.Flag] || arg.Type != "duration" {
				return fmt.Errorf("config: operation[%d]: invalid argument grammar", i)
			}
			argSeen[arg.Flag] = true
			min, err := time.ParseDuration(arg.Min)
			if err != nil {
				return fmt.Errorf("config: operation[%d]: invalid argument min", i)
			}
			max, err := time.ParseDuration(arg.Max)
			if err != nil || max < min {
				return fmt.Errorf("config: operation[%d]: invalid argument max", i)
			}
		}
		for routeIndex, route := range c.Routes {
			if equalCommand(route.CredentialCommand, op.CredentialCommand) {
				return fmt.Errorf("config: route[%d] shares a credential command with closed operation[%d]", routeIndex, i)
			}
		}
	}
	return nil
}

func equalCommand(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidateClientIDs cross-checks allowed_client_ids against the token IDs
// actually loaded (pure). Every referenced id must exist as a named token —
// an allowlist entry that can never match is a policy typo, not a policy.
func ValidateClientIDs(routes []Route, tokenIDs []string) error {
	known := make(map[string]bool, len(tokenIDs))
	for _, id := range tokenIDs {
		known[id] = true
	}
	for i, r := range routes {
		for _, id := range r.AllowedClientIDs {
			if !known[id] {
				return fmt.Errorf("config: route[%d] %s: allowed_client_ids %q does not match any token id", i, r.Path, id)
			}
		}
	}
	return nil
}
