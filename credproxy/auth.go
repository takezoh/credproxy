package credproxy

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// tokenIDKey is the context key for the authenticated token's identifier.
type tokenIDKey struct{}

// extractBearer extracts the bearer token from a slice of Authorization header values.
// Returns ("", false) if the header is absent, empty, has multiple values, or does not
// begin with a case-insensitive "Bearer " prefix.
func extractBearer(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	v := values[0]
	if len(v) < 7 || !strings.EqualFold(v[:7], "bearer ") {
		return "", false
	}
	token := strings.TrimSpace(v[7:])
	if token == "" {
		return "", false
	}
	return token, true
}

// matchTokenEntries returns the id of the first entry whose token matches presented.
// All comparisons use constant-time byte equality to reduce timing side-channels.
func matchTokenEntries(entries []tokenEntry, presented []byte) (id string, ok bool) {
	for _, e := range entries {
		if subtle.ConstantTimeCompare(e.token, presented) == 1 {
			return e.id, true
		}
	}
	return "", false
}

// matchToken returns the id of the matching token entry, holding the read lock during comparison.
func (s *Server) matchToken(presented []byte) (id string, ok bool) {
	s.tokensMu.RLock()
	defer s.tokensMu.RUnlock()
	return matchTokenEntries(s.tokens, presented)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AllowUnauthenticated || s.tokenCount() == 0 {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := extractBearer(r.Header.Values("Authorization"))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, matched := s.matchToken([]byte(token))
		if !matched {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), tokenIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
