package credproxy

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

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

// matchToken reports whether presented matches any token in the set.
// All comparisons use constant-time byte equality to reduce timing side-channels.
func matchToken(tokens [][]byte, presented []byte) bool {
	for _, t := range tokens {
		if subtle.ConstantTimeCompare(t, presented) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AllowUnauthenticated || len(s.tokens) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := extractBearer(r.Header.Values("Authorization"))
		if !ok || !matchToken(s.tokens, []byte(token)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
