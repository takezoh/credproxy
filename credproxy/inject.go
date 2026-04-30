package credproxy

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
	deleteHeaders []string
	setQuery      map[string]string
}

// planRequest computes the mutations needed to inject credentials into an upstream request.
// It does not touch any http.Request; the caller applies the plan with applyPlan.
func planRequest(cfg Route, inj *Injection) requestPlan {
	p := requestPlan{
		setHeaders: make(map[string]string, len(inj.Headers)),
		setQuery:   make(map[string]string, len(inj.Query)),
	}
	if cfg.StripInboundAuth {
		p.deleteHeaders = []string{"Authorization"}
	}
	for k, v := range inj.Headers {
		p.setHeaders[k] = v
	}
	for k, v := range inj.Query {
		p.setQuery[k] = v
	}
	return p
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
