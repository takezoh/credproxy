package config

import "fmt"

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
	return nil
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
