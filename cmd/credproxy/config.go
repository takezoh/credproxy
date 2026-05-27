package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Hook           []string `toml:"hook"`
	HookTimeoutSec int      `toml:"hook_timeout_sec"`
}

func (c Config) hookTimeout() time.Duration {
	if c.HookTimeoutSec <= 0 {
		return 10 * time.Second
	}
	return time.Duration(c.HookTimeoutSec) * time.Second
}

func defaultConfigPath() string {
	if p := os.Getenv("CREDPROXY_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "credproxy", "config.toml")
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("load config %s: %w", path, err)
	}
	if len(cfg.Hook) == 0 {
		return cfg, fmt.Errorf("config %s: hook is required", path)
	}
	return cfg, nil
}
