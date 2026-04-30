package credproxy

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// Provider supplies credentials for one route.
// It must be safe for concurrent use.
type Provider interface {
	// Get returns credentials for the current request.
	// Implementations are responsible for their own caching and refresh logic.
	Get(ctx context.Context, req Request) (*Injection, error)

	// Refresh forces a credential refresh, bypassing any cache.
	// Called automatically when the upstream returns a status in RefreshOnStatus.
	Refresh(ctx context.Context, req Request) (*Injection, error)
}

// Request describes the inbound HTTP request being proxied.
type Request struct {
	Method   string
	Path     string
	Host     string
	// Metadata carries caller-supplied key/value pairs forwarded to Providers.
	// The library never interprets these values (provider-agnostic).
	Metadata map[string]string
}

// Injection describes what the proxy should inject before forwarding.
type Injection struct {
	// Headers are merged into the upstream request.
	Headers map[string]string
	// Query params are merged into the upstream URL.
	Query map[string]string
	// BodyReplace, when non-nil, is returned directly to the client without upstream forwarding.
	BodyReplace []byte
	// ExpiresAt, when non-zero, is informational (Providers manage their own TTL internally).
	ExpiresAt time.Time
}

// Store is a simple key/value byte store used by Providers.
// Implementations must be safe for concurrent use.
type Store interface {
	Load(ctx context.Context, key string) ([]byte, error)
	Save(ctx context.Context, key string, data []byte) error
}

// Route maps a path prefix to a provider and upstream.
type Route struct {
	// Path is the URL prefix to match, e.g. "/anthropic".
	Path string
	// Upstream is the target URL, e.g. "https://api.anthropic.com".
	// Empty for body_replace-only routes (e.g. AWS credential endpoints).
	Upstream string
	// Provider supplies credentials for this route.
	Provider Provider
	// RefreshOnStatus lists HTTP status codes that trigger a credential refresh + retry.
	RefreshOnStatus []int
	// StripInboundAuth removes the inbound Authorization header before injection.
	StripInboundAuth bool
}

// TokenAuth pairs a bearer token with a caller-assigned identifier.
// The identifier is forwarded to providers via Request.Metadata["token_id"],
// enabling per-token access control without exposing the raw token value.
type TokenAuth struct {
	Token string // bearer token value
	ID    string // identifier passed to providers via Request.Metadata["token_id"]
}

// ServerConfig configures the proxy server.
type ServerConfig struct {
	// ListenTCP is the TCP address to listen on, e.g. "127.0.0.1:9787".
	// Use "127.0.0.1:0" for an ephemeral port; call Server.Addr() after New() to get the actual address.
	// Empty means no TCP listener.
	ListenTCP string
	// ListenUnix is the Unix socket path. Empty means no Unix listener.
	ListenUnix string
	// UnixMode is the file mode applied to the Unix socket. Defaults to 0600.
	UnixMode os.FileMode
	// AuthTokens is the set of valid bearer tokens.
	// If non-empty, every route request must carry a matching Authorization: Bearer header.
	// If empty, AllowUnauthenticated must be true; otherwise New() returns an error.
	AuthTokens []TokenAuth
	// AllowUnauthenticated disables bearer authentication entirely when true.
	// Should only be set for Unix-socket-only servers protected by OS file permissions.
	AllowUnauthenticated bool
	// Routes defines the proxy routes.
	Routes []Route
	// ShutdownTimeout is the maximum time to wait for in-flight requests when the context is
	// cancelled. Defaults to 15 seconds.
	ShutdownTimeout time.Duration
	// Logger, if nil, uses the default slog logger.
	Logger *slog.Logger
}
