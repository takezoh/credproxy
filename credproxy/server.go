package credproxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultShutdownTimeout = 15 * time.Second
const defaultUnixMode = 0o600

// tokenEntry stores a single bearer token and its caller-assigned identifier.
type tokenEntry struct {
	token []byte
	id    string
}

// Server is the credential proxy HTTP server.
type Server struct {
	cfg       ServerConfig
	log       *slog.Logger
	tokensMu  sync.RWMutex
	tokens    []tokenEntry
	mux       *http.ServeMux
	listeners []net.Listener
	tcpAddr   string
}

// New creates and binds a Server. Listeners are opened immediately so that
// Addr() returns the resolved address before Run() is called.
func New(cfg ServerConfig) (*Server, error) {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}


	entries := make([]tokenEntry, len(cfg.AuthTokens))
	for i, t := range cfg.AuthTokens {
		entries[i] = tokenEntry{token: []byte(t.Token), id: t.ID}
	}

	s := &Server{
		cfg:    cfg,
		log:    log,
		tokens: entries,
		mux:    http.NewServeMux(),
	}

	if err := s.registerRoutes(); err != nil {
		return nil, err
	}
	if err := s.openListeners(); err != nil {
		return nil, err
	}
	if len(s.listeners) == 0 {
		return nil, fmt.Errorf("credproxy: no listeners configured")
	}
	return s, nil
}

// Addr returns the resolved TCP listen address (e.g. "127.0.0.1:PORT").
// Useful when ListenTCP was "127.0.0.1:0" (ephemeral port).
func (s *Server) Addr() string { return s.tcpAddr }

// AddAuthToken registers a bearer token with the given id.
// Idempotent: an existing entry with the same id is replaced.
// Safe for concurrent use; may be called after New() to register tokens dynamically.
func (s *Server) AddAuthToken(token, id string) {
	entry := tokenEntry{token: []byte(token), id: id}
	s.tokensMu.Lock()
	defer s.tokensMu.Unlock()
	for i, e := range s.tokens {
		if e.id == id {
			s.tokens[i] = entry
			return
		}
	}
	s.tokens = append(s.tokens, entry)
}

func (s *Server) tokenCount() int {
	s.tokensMu.RLock()
	n := len(s.tokens)
	s.tokensMu.RUnlock()
	return n
}

// Handler returns the underlying http.Handler (useful for testing without listeners).
func (s *Server) Handler() http.Handler { return s.mux }

// Run starts serving on the already-opened listeners and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Handler: s.mux}

	errCh := make(chan error, len(s.listeners))
	for _, ln := range s.listeners {
		go func(l net.Listener) {
			if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}(ln)
	}

	select {
	case <-ctx.Done():
		timeout := s.cfg.ShutdownTimeout
		if timeout <= 0 {
			timeout = defaultShutdownTimeout
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) registerRoutes() error {
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	for _, r := range s.cfg.Routes {
		h, err := newRouteHandler(r, s.log)
		if err != nil {
			return fmt.Errorf("credproxy: route %s: %w", r.Path, err)
		}
		pattern := r.Path
		if !strings.HasSuffix(pattern, "/") {
			pattern += "/"
		}
		s.mux.Handle(pattern, s.authMiddleware(http.StripPrefix(r.Path, h)))
	}
	return nil
}

func (s *Server) openListeners() error {
	if addr := s.cfg.ListenTCP; addr != "" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("credproxy: listen tcp %s: %w", addr, err)
		}
		s.tcpAddr = ln.Addr().String()
		s.log.Info("credproxy: listening", "tcp", s.tcpAddr)
		s.listeners = append(s.listeners, ln)
	}

	if path := s.cfg.ListenUnix; path != "" {
		if err := s.openUnixListener(path); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) openUnixListener(path string) error {
	// Guard against accidentally removing a non-socket file.
	if info, err := os.Stat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("credproxy: listen unix %s: path exists and is not a socket", path)
		}
		_ = os.Remove(path)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("credproxy: listen unix %s: %w", path, err)
	}

	mode := s.cfg.UnixMode
	if mode == 0 {
		mode = defaultUnixMode
	}
	if err := os.Chmod(path, mode); err != nil {
		_ = ln.Close()
		return fmt.Errorf("credproxy: chmod unix %s: %w", path, err)
	}

	s.log.Info("credproxy: listening", "unix", path, "mode", fmt.Sprintf("%04o", mode))
	s.listeners = append(s.listeners, ln)
	return nil
}
