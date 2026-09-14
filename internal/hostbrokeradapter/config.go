// Package hostbrokeradapter exposes credproxy's read-only route inventory to
// Host Broker without moving credential transport policy into credproxy core.
package hostbrokeradapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Config is the host-local adapter configuration written by the lifecycle owner.
type Config struct {
	Version             int               `json:"version"`
	Listen              string            `json:"listen"`
	TLSServerName       string            `json:"tls_server_name"`
	TLSCertFile         string            `json:"tls_cert_file"`
	TLSKeyFile          string            `json:"tls_key_file"`
	BearerFile          string            `json:"bearer_file"`
	StateFile           string            `json:"state_file"`
	BackendURL          string            `json:"backend_url"`
	BackendUnixSocket   string            `json:"backend_unix_socket"`
	PrincipalTokenFiles map[string]string `json:"principal_token_files"`
	Registration        Registration      `json:"registration"`
}

// Registration describes the outbound service-registration lease.
type Registration struct {
	BrokerURL           string `json:"broker_url"`
	TokenFile           string `json:"token_file"`
	Service             string `json:"service"`
	EndpointRef         string `json:"endpoint_ref"`
	ContractVersion     string `json:"contract_version"`
	RenewIntervalSecond int    `json:"renew_interval_seconds"`
}

// LoadConfig reads one exact, owner-only adapter configuration.
func LoadConfig(path string) (Config, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return Config{}, errors.New("credproxy adapter config identity is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Config{}, errors.New("credproxy adapter config has trailing data")
	}
	if cfg.Version != 1 || cfg.Listen == "" || cfg.TLSServerName == "" || cfg.BackendURL == "" || len(cfg.PrincipalTokenFiles) == 0 {
		return Config{}, errors.New("credproxy adapter config is incomplete")
	}
	for _, candidate := range []string{cfg.TLSCertFile, cfg.TLSKeyFile, cfg.BearerFile, cfg.StateFile, cfg.BackendUnixSocket, cfg.Registration.TokenFile} {
		if !filepath.IsAbs(candidate) {
			return Config{}, fmt.Errorf("credproxy adapter path must be absolute: %s", candidate)
		}
	}
	for principal, tokenFile := range cfg.PrincipalTokenFiles {
		if principal == "" || !filepath.IsAbs(tokenFile) {
			return Config{}, errors.New("credproxy adapter principal mapping is invalid")
		}
	}
	if cfg.Registration.Service != "credproxy" || cfg.Registration.EndpointRef == "" || cfg.Registration.ContractVersion != "1" || cfg.Registration.RenewIntervalSecond < 1 {
		return Config{}, errors.New("credproxy adapter registration is invalid")
	}
	return cfg, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
