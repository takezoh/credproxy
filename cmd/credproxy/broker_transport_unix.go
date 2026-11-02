//go:build !windows

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

var brokerDialer = func(ctx context.Context, endpoint, _ string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint)
}

func defaultBrokerEndpoint() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		if runtime.GOOS == "darwin" {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, "Library", "Caches", "credproxyd", "runtime", "credproxyd", "broker.sock"), nil
		}
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "credproxyd", "broker.sock"), nil
}

func brokerEndpointReady(endpoint string) error {
	_, err := os.Stat(endpoint)
	return err
}

func defaultExecEndpoint() string { return "" }

func expectedServerExecutable(explicit string) (string, error) { return explicit, nil }
