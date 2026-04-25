package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// envFuncs holds injectable environment accessors for pure expansion.
type envFuncs struct {
	getenv func(string) string
	home   string // resolved home directory; empty string triggers an error on ~/... paths
}

// expand returns a copy of c with all environment variables and ~ paths resolved.
// No I/O is performed; all external state is supplied via e.
func expand(c Config, e envFuncs) (Config, error) {
	var err error
	c.ListenTCP = e.getenv(c.ListenTCP)
	c.ListenUnix, err = expandPath(e.getenv(c.ListenUnix), e.home)
	if err != nil {
		return c, fmt.Errorf("listen_unix: %w", err)
	}
	c.AuthTokensFile, err = expandPath(e.getenv(c.AuthTokensFile), e.home)
	if err != nil {
		return c, fmt.Errorf("auth_tokens_file: %w", err)
	}
	for i := range c.Routes {
		r := &c.Routes[i]
		r.Upstream = e.getenv(r.Upstream)
		if r.HookTimeoutSec <= 0 {
			r.HookTimeoutSec = 10
		}
		for j, arg := range r.CredentialCommand {
			r.CredentialCommand[j] = e.getenv(arg)
		}
		for j, arg := range r.RefreshCommand {
			r.RefreshCommand[j] = e.getenv(arg)
		}
	}
	return c, nil
}

// expandPath resolves ~ prefix using the provided home directory.
// Returns an error when the path starts with ~/ but home is empty.
func expandPath(p, home string) (string, error) {
	if !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	if home == "" {
		return "", fmt.Errorf("path %q requires home directory but none is available", p)
	}
	return filepath.Join(home, p[2:]), nil
}
