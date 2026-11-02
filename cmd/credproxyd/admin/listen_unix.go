//go:build !windows

package admin

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// DefaultSocketPath returns the private runtime socket for daemon administration.
func DefaultSocketPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		if runtime.GOOS != "darwin" {
			return "", errors.New("XDG_RUNTIME_DIR is required for the admin socket")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("admin socket home directory: %w", err)
		}
		dir = filepath.Join(home, "Library", "Caches", "credproxyd", "runtime")
	}
	return filepath.Join(dir, "credproxyd", "admin.sock"), nil
}

func listen(socket string) (net.Listener, error) {
	if socket == "" {
		return nil, errors.New("admin socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return nil, fmt.Errorf("create admin socket directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(socket), 0700); err != nil {
		return nil, fmt.Errorf("protect admin socket directory: %w", err)
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("admin socket path is not a socket: %s", socket)
		}
		conn, dialErr := net.DialTimeout("unix", socket, time.Second)
		if dialErr == nil {
			conn.Close()
			return nil, fmt.Errorf("admin socket already in use: %s", socket)
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("check existing admin socket: %w", dialErr)
		}
		if err := os.Remove(socket); err != nil {
			return nil, fmt.Errorf("remove stale admin socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen admin socket: %w", err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		listener.Close()
		os.Remove(socket)
		return nil, fmt.Errorf("protect admin socket: %w", err)
	}
	return listener, nil
}

func cleanupEndpoint(socket string) { _ = os.Remove(socket) }
