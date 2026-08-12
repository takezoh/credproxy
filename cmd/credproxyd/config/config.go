package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration for credproxyd.
type Config struct {
	ListenTCP      string      `toml:"listen_tcp"`
	ListenUnix     string      `toml:"listen_unix"`
	LogLevel       string      `toml:"log_level"`
	DaemonRevision string      `toml:"daemon_revision"`
	AuthTokensFile string      `toml:"auth_tokens_file"`
	Routes         []Route     `toml:"route"`
	Operations     []Operation `toml:"operation"`
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
	AllowedClientIDs  []string `toml:"allowed_client_ids"`
}

// Operation defines a closed daemon-side command. Credential values are
// injected into the child and are never serialized to the caller.
type Operation struct {
	Name              string              `toml:"name"`
	BindingRevision   string              `toml:"binding_revision"`
	ExecutablePaths   []string            `toml:"executable_paths"`
	Subcommand        string              `toml:"subcommand"`
	CredentialCommand []string            `toml:"credential_command"`
	HookTimeoutSec    int                 `toml:"hook_timeout_sec"`
	MaxRuntimeSec     int                 `toml:"max_runtime_sec"`
	Environment       map[string]string   `toml:"env"`
	FixedEnv          map[string]string   `toml:"fixed_env"`
	PassEnv           []string            `toml:"pass_env"`
	Arguments         []OperationArgument `toml:"argument"`
}

type OperationArgument struct {
	Flag string `toml:"flag"`
	Type string `toml:"type"`
	Min  string `toml:"min"`
	Max  string `toml:"max"`
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

// Token is one entry of the auth tokens file.
type Token struct {
	// ID is the client name from "<id>=<token>" lines; empty for bare token
	// lines (the caller assigns a positional fallback). Only named tokens can
	// be referenced from allowed_client_ids.
	ID    string
	Value string
}

// LoadTokens reads bearer tokens from the file, one per line.
// A line may name its client as "<id>=<token>"; bare lines stay unnamed.
func LoadTokens(path string) ([]Token, error) {
	home, _ := os.UserHomeDir()
	p, err := expandPath(os.ExpandEnv(path), home)
	if err != nil {
		return nil, fmt.Errorf("config: tokens file: %w", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("config: tokens file %s: %w", p, err)
	}
	return parseTokens(string(data))
}

// parseTokens parses the tokens file body (pure).
func parseTokens(data string) ([]Token, error) {
	var tokens []Token
	seen := make(map[string]bool)
	for n, line := range strings.Split(data, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		i := strings.IndexByte(t, '=')
		if i < 0 {
			tokens = append(tokens, Token{Value: t})
			continue
		}
		id := strings.TrimSpace(t[:i])
		value := strings.TrimSpace(t[i+1:])
		if id == "" || value == "" {
			return nil, fmt.Errorf("config: tokens file line %d: expected <id>=<token>", n+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("config: tokens file line %d: duplicate token id %q", n+1, id)
		}
		seen[id] = true
		tokens = append(tokens, Token{ID: id, Value: value})
	}
	return tokens, nil
}
