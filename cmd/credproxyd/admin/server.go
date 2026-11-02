package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
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

func serve(ctx context.Context, endpoint string, listener net.Listener, refresh Refresher) error {
	defer listener.Close()
	defer cleanupEndpoint(endpoint)
	server := &http.Server{Handler: Handler(refresh), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	err := server.Serve(protectListener(listener))
	close(done)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
