package credproxy

import (
	"context"
	"encoding/json"
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

// reservedPaths are endpoints the server itself owns. A configured route may
// not claim one: silently shadowing liveness or discovery would turn a config
// typo into a broken control, so New() rejects the collision instead.
var reservedPaths = map[string]bool{
	"/healthz": true,
	"/_routes": true,
}

// tokenEntry stores a single bearer token and its caller-assigned identifier.
type tokenEntry struct {
	token []byte
	id    string
}

// PeriodicJob is a background task that runs on a fixed interval while the server is running.
// RegisterPeriodic must be called before Run; jobs start when Run is called.
type PeriodicJob struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// Server is the credential proxy HTTP server.
type Server struct {
	cfg          ServerConfig
	log          *slog.Logger
	selfUID      uint32
	tokensMu     sync.RWMutex
	tokens       []tokenEntry
	mux          *http.ServeMux
	listeners    []net.Listener
	tcpAddr      string
	periodicJobs []PeriodicJob
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

	if cfg.RequireSamePeerUID {
		if !peerCredSupported {
			return nil, errPeerCredUnsupported
		}
		if cfg.ListenTCP != "" {
			return nil, fmt.Errorf("credproxy: RequireSamePeerUID cannot be enforced on a TCP listener")
		}
	}

	// A per-route client restriction is meaningless without bearer identities:
	// with AllowUnauthenticated every request has an empty client id and the
	// restriction would deny everything while looking like a working control.
	for _, r := range cfg.Routes {
		if len(r.AllowedClientIDs) > 0 && cfg.AllowUnauthenticated {
			return nil, fmt.Errorf("credproxy: route %s: AllowedClientIDs requires bearer authentication (AllowUnauthenticated is set)", r.Path)
		}
	}

	s := &Server{
		cfg:     cfg,
		log:     log,
		selfUID: uint32(os.Getuid()),
		tokens:  entries,
		mux:     http.NewServeMux(),
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

// RegisterPeriodic adds a job to be run on a fixed interval. Must be called before Run.
func (s *Server) RegisterPeriodic(j PeriodicJob) {
	s.periodicJobs = append(s.periodicJobs, j)
}

// Run starts serving on the already-opened listeners and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Handler: s.mux, ConnContext: connContext}

	go s.runScheduler(ctx)

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

func (s *Server) runScheduler(ctx context.Context) {
	if len(s.periodicJobs) == 0 {
		return
	}
	type entry struct {
		job      PeriodicJob
		nextFire time.Time
	}
	entries := make([]entry, len(s.periodicJobs))
	now := time.Now()
	for i, j := range s.periodicJobs {
		entries[i] = entry{j, now.Add(j.Every)}
	}
	for {
		next := entries[0].nextFire
		for _, e := range entries[1:] {
			if e.nextFire.Before(next) {
				next = e.nextFire
			}
		}
		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		now = time.Now()
		for i := range entries {
			if !entries[i].nextFire.After(now) {
				if err := entries[i].job.Run(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("credproxy: periodic job failed", "job", entries[i].job.Name, "err", err)
				}
				entries[i].nextFire = time.Now().Add(entries[i].job.Every)
			}
		}
	}
}

func (s *Server) registerRoutes() error {
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	s.mux.Handle("/_routes", s.authMiddleware(http.HandlerFunc(s.handleRouteList)))
	for _, r := range s.cfg.Routes {
		if reservedPaths[strings.TrimSuffix(r.Path, "/")] {
			return fmt.Errorf("credproxy: route %s: path is reserved by the server", r.Path)
		}
		h, err := newRouteHandler(r, s.log)
		if err != nil {
			return fmt.Errorf("credproxy: route %s: %w", r.Path, err)
		}
		wrapped := s.authMiddleware(http.StripPrefix(r.Path, h))
		s.mux.Handle(r.Path, wrapped)
		if !strings.HasSuffix(r.Path, "/") {
			// subtreeだけを登録するとServeMuxがexact pathをslash付きへredirectし、
			// upstreamにも余分なslashが渡る。exact endpointとsubtreeを分けて登録する。
			s.mux.Handle(r.Path+"/", wrapped)
		}
	}
	return nil
}

// handleRouteList answers the reserved /_routes endpoint with the routes this
// caller may fetch a credential body from, so a client can consume every route
// without being told their names out of band — adding a credential becomes a
// route definition, not a change on both sides.
//
// Two filters apply. Routes with an upstream are omitted because discovery must
// have no side effects: a client that probed one would send a real request to
// the upstream with a real credential attached. Routes restricted by
// AllowedClientIDs are omitted for callers not on the list, so the listing never
// advertises what the caller would only get a 403 from.
//
// Route names carry no secret: a client must already know a name to use it.
func (s *Server) handleRouteList(w http.ResponseWriter, r *http.Request) {
	clientID, _ := r.Context().Value(tokenIDKey{}).(string)
	names := make([]string, 0, len(s.cfg.Routes))
	for _, rt := range s.cfg.Routes {
		if rt.Upstream != "" {
			continue
		}
		if len(rt.AllowedClientIDs) > 0 && !clientAllowed(rt.AllowedClientIDs, clientID) {
			continue
		}
		names = append(names, strings.TrimPrefix(strings.TrimSuffix(rt.Path, "/"), "/"))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Routes []string `json:"routes"`
	}{Routes: names})
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
