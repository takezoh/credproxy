package hostbrokeradapter

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const protocolPrefix = "/private.hostbroker.adaptercontrol.v1.AdapterControl/"

type wireKey struct {
	Principal string `json:"principal"`
	Service   string `json:"service"`
	Operation string `json:"operation"`
	RequestID string `json:"request_id"`
}

type caller struct {
	Principal          string `json:"principal"`
	Role               string `json:"role,omitempty"`
	CorrelationID      string `json:"correlation_id,omitempty"`
	PolicyRevision     uint64 `json:"policy_revision,omitempty"`
	InstanceGeneration uint64 `json:"instance_generation"`
	DefinitionRevision uint64 `json:"definition_revision,omitempty"`
}

type request struct {
	Key                 wireKey `json:"key"`
	Caller              caller  `json:"caller"`
	ContractMajor       uint32  `json:"contract_major"`
	Input               string  `json:"input,omitempty"`
	ResponseWindowBytes uint64  `json:"response_window_bytes"`
	DeadlineUnixNano    int64   `json:"deadline_unix_nano,omitempty"`
}

// Application is the private protocol handler used by the process and tests.
type Application struct {
	cfg     Config
	bearer  string
	journal *journal
	client  *http.Client
}

// NewApplication validates the open-backend probe before accepting broker traffic.
func NewApplication(ctx context.Context, cfg Config) (*Application, error) {
	bearer, err := readSecret(cfg.BearerFile)
	if err != nil {
		return nil, err
	}
	j, err := openJournal(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if cfg.BackendUnixSocket != "" {
		client.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", cfg.BackendUnixSocket)
		}}
	}
	a := &Application{cfg: cfg, bearer: bearer, journal: j, client: client}
	probe, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BackendURL, "/")+"/_routes", nil)
	if err != nil {
		return nil, err
	}
	response, err := a.client.Do(probe)
	if err != nil {
		return nil, fmt.Errorf("credproxy adapter startup probe: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		return nil, fmt.Errorf("credproxy adapter startup probe rejected status %d", response.StatusCode)
	}
	return a, nil
}

func readSecret(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
		return "", errors.New("credproxy adapter secret identity is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("credproxy adapter secret is invalid")
	}
	return value, nil
}

func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, protocolPrefix) || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Host-Broker-Private-Protocol") != "1" {
		http.NotFound(w, r)
		return
	}
	want := "Bearer " + a.bearer
	got := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "rejected"})
		return
	}
	method := strings.TrimPrefix(r.URL.Path, protocolPrefix)
	limited := io.LimitReader(r.Body, 1<<20+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var req request
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF || req.Key.Principal == "" || req.Key.Principal != req.Caller.Principal || req.Key.Service != "credproxy" || req.ResponseWindowBytes == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "rejected"})
		return
	}
	key := strings.Join([]string{fmt.Sprint(req.Caller.InstanceGeneration), req.Key.Principal, req.Key.Service, req.Key.Operation, req.Key.RequestID}, "\x00")
	input, err := base64.StdEncoding.DecodeString(req.Input)
	if req.Input == "" {
		input = []byte("{}")
		err = nil
	}
	if err != nil || !json.Valid(input) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "rejected"})
		return
	}
	switch method {
	case "ResolveIdempotency":
		if _, ok := a.cfg.PrincipalTokenFiles[req.Key.Principal]; !ok {
			writeJSON(w, http.StatusOK, map[string]any{"kind": "mapping_unresolved"})
			return
		}
		kind, output, err := a.journal.resolve(key, input)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"kind": "mapping_unresolved"})
			return
		}
		body := map[string]any{"kind": kind}
		if kind == "replay" {
			body["stored"] = map[string]any{"kind": "completed", "result": base64.StdEncoding.EncodeToString(output)}
		}
		writeJSON(w, http.StatusOK, body)
	case "Invoke":
		a.invoke(w, r, req, key, input)
	case "ConfirmAccepted":
		writeJSON(w, http.StatusOK, map[string]any{"known": false})
	case "GetOperation", "CancelOperation":
		writeJSON(w, http.StatusOK, map[string]any{"kind": "ownership_unknown"})
	case "LookupRequest":
		writeJSON(w, http.StatusOK, map[string]any{"kind": "execution_unknown"})
	default:
		http.NotFound(w, r)
	}
}

func (a *Application) invoke(w http.ResponseWriter, incoming *http.Request, req request, key string, input []byte) {
	if req.ContractMajor != 1 || req.Key.Operation != "list_routes" {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "invalid_argument"})
		return
	}
	tokenPath, ok := a.cfg.PrincipalTokenFiles[req.Key.Principal]
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "permission_denied", "reason": "not_visible"})
		return
	}
	token, err := readSecret(tokenPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "unavailable"})
		return
	}
	endpoint, _ := url.JoinPath(strings.TrimRight(a.cfg.BackendURL, "/"), "/_routes")
	backendRequest, _ := http.NewRequestWithContext(incoming.Context(), http.MethodGet, endpoint, nil)
	backendRequest.Header.Set("Authorization", "Bearer "+token)
	response, err := a.client.Do(backendRequest)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "unavailable"})
		return
	}
	defer response.Body.Close()
	output, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if readErr != nil || len(output) > 1<<20 || uint64(len(output)) > req.ResponseWindowBytes {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "resource_exhausted"})
		return
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "permission_denied", "reason": "not_visible"})
		return
	}
	if response.StatusCode != http.StatusOK || !json.Valid(output) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "unavailable"})
		return
	}
	if err := a.journal.complete(key, input, output); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "failed", "class": "resource_exhausted"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "completed", "output": base64.StdEncoding.EncodeToString(output)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
