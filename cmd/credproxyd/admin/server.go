package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Refresher replaces cached credentials. The implementation owns atomic replacement.
type Refresher interface {
	Refresh(context.Context, []string, bool) error
}

type refreshRequest struct {
	Credential   string `json:"credential"`
	AllPreloaded bool   `json:"all_preloaded"`
}

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

// Handler exposes only credential refresh; it never returns credential values.
func Handler(refresh Refresher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /refresh", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var req refreshRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid refresh request", http.StatusBadRequest)
			return
		}
		if err := dec.Decode(new(any)); err == nil {
			http.Error(w, "invalid refresh request", http.StatusBadRequest)
			return
		} else if !errors.Is(err, io.EOF) {
			http.Error(w, "invalid refresh request", http.StatusBadRequest)
			return
		}
		if req.AllPreloaded == (req.Credential != "") || strings.TrimSpace(req.Credential) != req.Credential {
			http.Error(w, "specify one credential or all_preloaded", http.StatusBadRequest)
			return
		}
		var names []string
		if req.Credential != "" {
			names = []string{req.Credential}
		}
		if err := refresh.Refresh(r.Context(), names, req.AllPreloaded); err != nil {
			slog.Error("credential refresh failed", "error", err)
			http.Error(w, "credential refresh failed; see daemon log", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// Start binds the admin socket before returning, so daemon startup can fail closed.
func Start(ctx context.Context, socket string, refresh Refresher) (<-chan error, error) {
	listener, err := listen(socket)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, socket, listener, refresh)
		close(done)
	}()
	return done, nil
}

// Serve starts a private Unix-only admin listener and stops it when ctx ends.
func Serve(ctx context.Context, socket string, refresh Refresher) error {
	listener, err := listen(socket)
	if err != nil {
		return err
	}
	return serve(ctx, socket, listener, refresh)
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

func serve(ctx context.Context, socket string, listener net.Listener, refresh Refresher) error {
	defer listener.Close()
	defer os.Remove(socket)
	server := &http.Server{Handler: Handler(refresh), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	err := server.Serve(&uidListener{Listener: listener})
	close(done)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
