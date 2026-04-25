package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration for credproxyd.
type Config struct {
	ListenTCP      string  `toml:"listen_tcp"`
	ListenUnix     string  `toml:"listen_unix"`
	LogLevel       string  `toml:"log_level"`
	AuthTokensFile string  `toml:"auth_tokens_file"`
	Routes         []Route `toml:"route"`
}

// Route maps an incoming path prefix to an upstream and hook script commands.
type Route struct {
	Path              string   `toml:"path"`
	Upstream          string   `toml:"upstream"`
	CredentialCommand []string `toml:"credential_command"`
	RefreshCommand    []string `toml:"refresh_command"`
	RefreshOnStatus   []int    `toml:"refresh_on_status"`
	HookTimeoutSec    int      `toml:"hook_timeout_sec"`
	StripInboundAuth  bool     `toml:"strip_inbound_auth"`
}

// Load reads, expands, and validates configuration from path.
func Load(path string) (*Config, error) {
	cfg := &Config{LogLevel: "info"}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("config: decode %s: %w", path, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // expandPath will error on ~/... paths
	}
	e := envFuncs{getenv: os.ExpandEnv, home: home}
	expanded, err := expand(*cfg, e)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &expanded, validate(expanded)
}

// LoadTokens reads bearer tokens from the file, one per line.
func LoadTokens(path string) ([]string, error) {
	home, _ := os.UserHomeDir()
	p, err := expandPath(os.ExpandEnv(path), home)
	if err != nil {
		return nil, fmt.Errorf("config: tokens file: %w", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("config: tokens file %s: %w", p, err)
	}
	var tokens []string
	for _, line := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens, nil
}
