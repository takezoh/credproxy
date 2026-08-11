package credproxy

import "errors"

// errPeerCredUnsupported is returned by peerCredOf on platforms without a
// peer-credential syscall. New refuses RequireSamePeerUID there (fail-closed).
var errPeerCredUnsupported = errors.New("credproxy: peer credentials unsupported on this platform")

// ReasonError attaches a machine-readable failure classification to a Provider error.
// The core never interprets Reason values (provider-agnostic): the vocabulary is owned
// by the Provider (e.g. credproxyd hook scripts), and the proxy passes the token through
// to the client in a structured 502 body so callers can distinguish failure classes
// (rate limit vs invalid token vs unreachable) without parsing log output.
type ReasonError struct {
	// Reason is a short machine-readable token, e.g. "op_rate_limited".
	// It is sent to clients verbatim; providers must not put secrets or
	// free-form detail here.
	Reason string
	// Err carries the full detail for server-side logs only.
	Err error
}

func (e *ReasonError) Error() string {
	if e.Err == nil {
		return e.Reason
	}
	return e.Reason + ": " + e.Err.Error()
}

func (e *ReasonError) Unwrap() error { return e.Err }
