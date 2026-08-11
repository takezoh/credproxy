package credproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// maxBodyBuffer is the maximum request-body size buffered for refresh-retry replay.
const maxBodyBuffer = 1 << 20 // 1 MiB

// errNeedsRefresh is a sentinel returned from ReverseProxy.ModifyResponse to trigger
// a credential refresh + retry via ErrorHandler.
var errNeedsRefresh = errors.New("credproxy: upstream returned refresh-triggering status")

// retryStateKey is the context key for per-request retry state.
type retryStateKey struct{}

// retryState carries per-request data needed by the ErrorHandler retry path.
type retryState struct {
	body     []byte
	proxyReq Request
	retried  bool
}

type routeHandler struct {
	cfg      Route
	proxy    *httputil.ReverseProxy
	log      *slog.Logger
	routeLog string
}

func newRouteHandler(cfg Route, log *slog.Logger) (*routeHandler, error) {
	h := &routeHandler{
		cfg:      cfg,
		log:      log,
		routeLog: strings.TrimPrefix(cfg.Path, "/"),
	}
	if cfg.Upstream != "" {
		upstream, err := url.Parse(cfg.Upstream)
		if err != nil {
			return nil, err
		}
		h.proxy = &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(upstream)
				r.Out.Host = upstream.Host
			},
			ModifyResponse: h.modifyResponse,
			ErrorHandler:   h.errorHandler,
		}
	}
	return h, nil
}

func (h *routeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Buffer body only when refresh-retry is possible (body replay requires a copy).
	var body []byte
	if len(h.cfg.RefreshOnStatus) > 0 {
		lr := io.LimitReader(r.Body, maxBodyBuffer+1)
		var err error
		body, err = io.ReadAll(lr)
		if err != nil {
			http.Error(w, "request read error", http.StatusBadRequest)
			return
		}
		// If the body exceeded the limit, forward once without enabling retry.
		if len(body) > maxBodyBuffer {
			body = body[:maxBodyBuffer]
		}
	}
	if r.Body != nil {
		_ = r.Body.Close()
	}

	clientID, _ := r.Context().Value(tokenIDKey{}).(string)
	if len(h.cfg.AllowedClientIDs) > 0 && !clientAllowed(h.cfg.AllowedClientIDs, clientID) {
		h.log.Warn("client not allowed for route", "route", h.routeLog, "client", clientID)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	proxyReq := Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Host:   r.Host,
	}
	if clientID != "" {
		proxyReq.Metadata = map[string]string{"token_id": clientID}
	}
	if pc, ok := r.Context().Value(peerCredKey{}).(peerCred); ok && pc.available {
		if proxyReq.Metadata == nil {
			proxyReq.Metadata = map[string]string{}
		}
		proxyReq.Metadata["peer_uid"] = strconv.FormatUint(uint64(pc.UID), 10)
		proxyReq.Metadata["peer_pid"] = strconv.FormatInt(int64(pc.PID), 10)
	}

	injection, err := h.cfg.Provider.Get(r.Context(), proxyReq)
	if err != nil {
		h.log.Error("provider.Get failed", "route", h.routeLog, "err", err)
		h.writeCredentialError(w, err, "credential error")
		return
	}

	switch decideAction(h.cfg, injection) {
	case actReturnBody:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(injection.BodyReplace)
		return
	case actNoUpstream:
		http.Error(w, "no upstream configured for route", http.StatusBadGateway)
		return
	}

	// Attach retry state so ErrorHandler can access body and proxyReq.
	state := &retryState{body: body, proxyReq: proxyReq}
	ctx := context.WithValue(r.Context(), retryStateKey{}, state)
	req := applyPlan(r.WithContext(ctx), body, planRequest(h.cfg, injection))
	h.proxy.ServeHTTP(w, req)
}

// modifyResponse inspects the upstream status; if it matches RefreshOnStatus the response
// body is discarded and errNeedsRefresh is returned to trigger ErrorHandler.
// For all other responses the body streams transparently to the client.
func (h *routeHandler) modifyResponse(resp *http.Response) error {
	if !needsRefresh(h.cfg.RefreshOnStatus, resp.StatusCode) {
		return nil
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(nil))
	return errNeedsRefresh
}

// errorHandler handles ReverseProxy errors, including the errNeedsRefresh sentinel.
func (h *routeHandler) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, errNeedsRefresh) {
		h.log.Error("proxy error", "route", h.routeLog, "err", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	state, _ := r.Context().Value(retryStateKey{}).(*retryState)
	if state == nil || state.retried {
		h.log.Warn("upstream auth failure after refresh", "route", h.routeLog)
		http.Error(w, "upstream auth failure after refresh", http.StatusBadGateway)
		return
	}

	refreshInj, err := h.cfg.Provider.Refresh(r.Context(), state.proxyReq)
	if err != nil {
		h.log.Error("provider.Refresh failed", "route", h.routeLog, "err", err)
		h.writeCredentialError(w, err, "credential refresh error")
		return
	}

	switch decideAction(h.cfg, refreshInj) {
	case actReturnBody:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(refreshInj.BodyReplace)
		return
	case actNoUpstream:
		http.Error(w, "no upstream configured for route", http.StatusBadGateway)
		return
	}

	state.retried = true
	req := applyPlan(r, state.body, planRequest(h.cfg, refreshInj))
	h.proxy.ServeHTTP(w, req)
}

// writeCredentialError reports a Provider failure to the client. Only the
// machine-readable reason of a ReasonError crosses the boundary as a structured
// body; every other detail (hook stderr, wrapped errors) stays in server logs so
// that a misbehaving hook cannot leak secrets through the client-facing response.
func (h *routeHandler) writeCredentialError(w http.ResponseWriter, err error, fallback string) {
	var re *ReasonError
	if !errors.As(err, &re) {
		http.Error(w, fallback, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "credential_unavailable",
		"route":  h.routeLog,
		"reason": re.Reason,
	})
}

// applyPlan returns a clone of r with the requestPlan mutations applied.
func applyPlan(r *http.Request, body []byte, p requestPlan) *http.Request {
	req := r.Clone(r.Context())
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	for _, k := range p.deleteHeaders {
		req.Header.Del(k)
	}
	for k, v := range p.setHeaders {
		req.Header.Set(k, v)
	}
	for k, v := range p.mergeHeaders {
		req.Header.Set(k, mergeCSV(req.Header.Get(k), v))
	}
	if len(p.setQuery) > 0 {
		q := req.URL.Query()
		for k, v := range p.setQuery {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	return req
}
