package credproxy

import "strings"

// action describes what the route handler should do with a resolved Injection.
type action int

const (
	// actForward injects headers/query into the request and forwards to upstream.
	actForward action = iota
	// actReturnBody returns Injection.BodyReplace directly to the client.
	actReturnBody
	// actNoUpstream means no upstream is configured and no BodyReplace is set — return 502.
	actNoUpstream
)

// decideAction returns the action to take for the given route configuration and injection.
func decideAction(cfg Route, inj *Injection) action {
	if len(inj.BodyReplace) > 0 {
		return actReturnBody
	}
	if cfg.Upstream == "" {
		return actNoUpstream
	}
	return actForward
}

// requestPlan holds the mutations to apply to an outbound request.
type requestPlan struct {
	setHeaders    map[string]string
	mergeHeaders  map[string]string
	deleteHeaders []string
	setQuery      map[string]string
}

// planRequest computes the mutations needed to inject credentials into an upstream request.
// It does not touch any http.Request; the caller applies the plan with applyPlan.
func planRequest(cfg Route, inj *Injection) requestPlan {
	p := requestPlan{
		setHeaders:   make(map[string]string, len(inj.Headers)),
		mergeHeaders: make(map[string]string, len(inj.AppendHeaders)),
		setQuery:     make(map[string]string, len(inj.Query)),
	}
	if cfg.StripInboundAuth {
		p.deleteHeaders = []string{"Authorization"}
	}
	for k, v := range inj.Headers {
		p.setHeaders[k] = v
	}
	for k, v := range inj.AppendHeaders {
		p.mergeHeaders[k] = v
	}
	for k, v := range inj.Query {
		p.setQuery[k] = v
	}
	return p
}

// mergeCSV merges add into existing as a comma-separated token list, preserving
// the order of existing tokens and appending only tokens not already present.
// Both arguments may themselves be comma-separated; surrounding spaces are trimmed.
func mergeCSV(existing, add string) string {
	out := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	appendToken := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		if _, dup := seen[tok]; dup {
			return
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	for _, tok := range strings.Split(existing, ",") {
		appendToken(tok)
	}
	for _, tok := range strings.Split(add, ",") {
		appendToken(tok)
	}
	return strings.Join(out, ", ")
}

// needsRefresh reports whether status is listed in refreshOn.
func needsRefresh(refreshOn []int, status int) bool {
	for _, s := range refreshOn {
		if s == status {
			return true
		}
	}
	return false
}
