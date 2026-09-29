package config

import (
	"fmt"
	"net/http"
	"strings"
)

// validate checks the expanded Config for required fields.
func validate(c Config) error {
	if c.ListenTCP == "" && c.ListenUnix == "" {
		return fmt.Errorf("config: at least one of listen_tcp or listen_unix must be set")
	}
	known := make(map[string]Credential)
	for i, s := range c.Credentials {
		if s.Name == "" || s.Provider != "onepassword" || !strings.HasPrefix(s.SecretRef, "op://") || (!s.Preload && s.TTLSec <= 0) {
			return fmt.Errorf("config: credential[%d]: invalid name, provider, secret_ref or ttl_sec", i)
		}
		if _, exists := known[s.Name]; exists {
			return fmt.Errorf("config: duplicate credential %q", s.Name)
		}
		known[s.Name] = s
	}
	if len(c.Credentials) > 0 && c.OnePasswordTokenFile == "" {
		return fmt.Errorf("config: onepassword_token_file is required")
	}
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("config: route[%d]: path is required", i)
		}
		if r.Upstream == "" && len(r.CredentialCommand) == 0 && r.Delivery != "env" {
			return fmt.Errorf("config: route[%d]: upstream or credential_command is required", i)
		}
		if r.Delivery == "" {
			if r.Credential != "" || len(r.Env) > 0 {
				return fmt.Errorf("config: route[%d]: delivery required", i)
			}
			continue
		}
		if len(r.CredentialCommand) > 0 || len(r.RefreshCommand) > 0 {
			return fmt.Errorf("config: route[%d]: script commands conflict with delivery", i)
		}
		switch r.Delivery {
		case "header":
			credential, exists := known[r.Credential]
			if !exists || r.Upstream == "" || r.Header == "" || !validHeader(r.Header) || strings.ContainsAny(r.Prefix, "\r\n") || len(r.Env) > 0 {
				return fmt.Errorf("config: route[%d]: invalid header delivery", i)
			}
			if credential.Preload && len(r.RefreshOnStatus) > 0 {
				return fmt.Errorf("config: route[%d]: preloaded credential cannot refresh on upstream status", i)
			}
		case "env":
			if r.Upstream != "" || r.Credential != "" || r.Header != "" || len(r.Env) == 0 {
				return fmt.Errorf("config: route[%d]: invalid env delivery", i)
			}
			for key, name := range r.Env {
				if _, exists := known[name]; key == "" || strings.ContainsAny(key, "=\x00\r\n") || !exists {
					return fmt.Errorf("config: route[%d]: invalid env mapping %q", i, key)
				}
			}
		default:
			return fmt.Errorf("config: route[%d]: unknown delivery %q", i, r.Delivery)
		}
	}
	return nil
}

func validHeader(s string) bool {
	return http.CanonicalHeaderKey(s) == s && !strings.ContainsAny(s, " \t\r\n:")
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
