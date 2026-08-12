package credproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	CredrouteProtocol       = "credroute/v1"
	operationPathPrefix     = "/v1/operations/"
	maxOperationRequestBody = 64 << 10
)

type operationRequest struct {
	Protocol        string   `json:"protocol"`
	BindingRevision string   `json:"binding_revision"`
	DaemonRevision  string   `json:"daemon_revision"`
	Arguments       []string `json:"arguments"`
}

type operationResponse struct {
	Protocol        string `json:"protocol"`
	BindingRevision string `json:"binding_revision"`
	DaemonRevision  string `json:"daemon_revision"`
	Operation       string `json:"operation"`
	Outcome         string `json:"outcome"`
	Error           string `json:"error,omitempty"`
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, executable string, args, env []string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

type operationHandler struct {
	op             Operation
	daemonRevision string
	log            *slog.Logger
}

func newOperationHandler(op Operation, daemonRevision string, log *slog.Logger) (*operationHandler, error) {
	if err := validateOperation(op); err != nil {
		return nil, err
	}
	if daemonRevision == "" {
		return nil, errors.New("daemon revision is required")
	}
	if op.Runner == nil {
		op.Runner = commandRunner{}
	}
	return &operationHandler{op: op, daemonRevision: daemonRevision, log: log}, nil
}

func validateOperation(op Operation) error {
	if !validOperationName(op.Name) || op.BindingRevision == "" || op.Subcommand == "" {
		return errors.New("name, binding revision, and subcommand are required")
	}
	if len(op.ExecutablePaths) == 0 || op.Provider == nil || len(op.Environment) == 0 {
		return errors.New("executable paths, provider, and environment are required")
	}
	for _, path := range op.ExecutablePaths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("executable path must be clean and absolute")
		}
	}
	seen := make(map[string]bool)
	for _, arg := range op.Arguments {
		if arg.Flag == "" || seen[arg.Flag] || arg.ValueType != "duration" || arg.Max < arg.Min {
			return fmt.Errorf("invalid argument grammar")
		}
		seen[arg.Flag] = true
	}
	for child, provider := range op.Environment {
		if !validEnvName(child) || !validEnvName(provider) {
			return errors.New("invalid environment name")
		}
		if _, exists := op.FixedEnv[child]; exists {
			return errors.New("credential and fixed environment overlap")
		}
	}
	for name := range op.FixedEnv {
		if !validEnvName(name) || unsafeRuntimeEnv(name) {
			return errors.New("unsafe fixed environment name")
		}
	}
	for _, name := range op.PassEnv {
		if !safePassEnv(name) {
			return errors.New("unsafe inherited environment name")
		}
	}
	return nil
}

func safePassEnv(name string) bool {
	return name == "LANG" || name == "TZ" || name == "TERM" || strings.HasPrefix(name, "LC_")
}

func unsafeRuntimeEnv(name string) bool {
	return strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") ||
		strings.HasPrefix(name, "PYTHON") || strings.HasPrefix(name, "RUBY") ||
		strings.HasPrefix(name, "PERL") || name == "NODE_OPTIONS" || name == "BASH_ENV" ||
		name == "ENV" || name == "SHELLOPTS"
}

func validOperationName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if (c >= 'A' && c <= 'Z') || c == '_' || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

func (h *operationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		h.write(w, http.StatusBadRequest, "rejected", "binding_request_invalid")
		return
	}
	req, err := decodeOperationRequest(r.Body)
	if err != nil {
		h.write(w, http.StatusBadRequest, "rejected", "binding_request_invalid")
		return
	}
	if req.Protocol != CredrouteProtocol || req.BindingRevision != h.op.BindingRevision || req.DaemonRevision != h.daemonRevision {
		h.write(w, http.StatusConflict, "rejected", "binding_mismatch")
		return
	}
	args, err := h.validateArguments(req.Arguments)
	if err != nil {
		h.write(w, http.StatusBadRequest, "rejected", "argument_rejected")
		return
	}
	h.execute(w, r, args)
}

func (h *operationHandler) execute(w http.ResponseWriter, r *http.Request, args []string) {
	executable, err := resolveExecutable(h.op.ExecutablePaths)
	if err != nil {
		h.write(w, http.StatusServiceUnavailable, "rejected", "binding_unavailable")
		return
	}
	providerCtx := r.Context()
	if h.op.CredentialTimeout > 0 {
		var cancel context.CancelFunc
		providerCtx, cancel = context.WithTimeout(providerCtx, h.op.CredentialTimeout)
		defer cancel()
	}
	injection, err := h.op.Provider.Get(providerCtx, Request{Method: r.Method, Path: r.URL.Path})
	if err != nil {
		h.log.Warn("closed operation credential unavailable", "operation", h.op.Name)
		h.write(w, http.StatusBadGateway, "failed", "credential_unavailable")
		return
	}
	env, err := h.buildEnvironment(injection)
	if err != nil {
		h.log.Warn("closed operation provider response invalid", "operation", h.op.Name)
		h.write(w, http.StatusBadGateway, "failed", "binding_response_invalid")
		return
	}
	runCtx := r.Context()
	if h.op.MaxRuntime > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, h.op.MaxRuntime)
		defer cancel()
	}
	err = h.op.Runner.Run(runCtx, executable, append([]string{h.op.Subcommand}, args...), env)
	if err == nil {
		h.write(w, http.StatusOK, "success", "")
		return
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		h.write(w, http.StatusGatewayTimeout, "timeout", "operation_timeout")
		return
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		h.write(w, http.StatusRequestTimeout, "cancelled", "operation_cancelled")
		return
	}
	h.log.Warn("closed operation child failed", "operation", h.op.Name)
	h.write(w, http.StatusBadGateway, "failed", "operation_failed")
}

func decodeOperationRequest(body io.Reader) (operationRequest, error) {
	var req operationRequest
	dec := json.NewDecoder(io.LimitReader(body, maxOperationRequestBody+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return req, errors.New("trailing request content")
	}
	return req, nil
}

func (h *operationHandler) validateArguments(input []string) ([]string, error) {
	grammar := make(map[string]OperationArgument, len(h.op.Arguments))
	for _, spec := range h.op.Arguments {
		grammar[spec.Flag] = spec
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for i := 0; i < len(input); i += 2 {
		if i+1 >= len(input) || seen[input[i]] {
			return nil, errors.New("missing value or duplicate argument")
		}
		spec, ok := grammar[input[i]]
		if !ok {
			return nil, errors.New("unknown argument")
		}
		value, err := time.ParseDuration(input[i+1])
		if err != nil || value < spec.Min || (spec.Max > 0 && value > spec.Max) {
			return nil, errors.New("argument value outside grammar")
		}
		seen[input[i]] = true
		result = append(result, input[i], input[i+1])
	}
	return result, nil
}

func resolveExecutable(paths []string) (string, error) {
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil && resolved == path {
			return path, nil
		}
	}
	return "", errors.New("no trusted executable available")
}

func (h *operationHandler) buildEnvironment(injection *Injection) ([]string, error) {
	if injection == nil || len(injection.Headers) != 0 || len(injection.AppendHeaders) != 0 || len(injection.Query) != 0 || injection.BodyReplace == nil {
		return nil, errors.New("provider returned unsupported injection")
	}
	bodyEnv, err := decodeExactEnv(injection.BodyReplace)
	if err != nil || len(bodyEnv) != len(h.op.Environment) {
		return nil, errors.New("provider env schema mismatch")
	}
	env := make(map[string]string, len(h.op.FixedEnv)+len(h.op.PassEnv)+len(h.op.Environment))
	for name, value := range h.op.FixedEnv {
		env[name] = value
	}
	for _, name := range h.op.PassEnv {
		if value, ok := os.LookupEnv(name); ok {
			env[name] = value
		}
	}
	used := make(map[string]bool, len(bodyEnv))
	for childName, providerName := range h.op.Environment {
		value, ok := bodyEnv[providerName]
		if !ok || used[providerName] {
			return nil, errors.New("provider env missing or duplicated")
		}
		used[providerName] = true
		env[childName] = value
	}
	for providerName := range bodyEnv {
		if !used[providerName] {
			return nil, errors.New("provider env contains unknown key")
		}
	}
	out := make([]string, 0, len(env))
	for name, value := range env {
		out = append(out, name+"="+value)
	}
	return out, nil
}

func decodeExactEnv(data []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errors.New("response must be an object")
	}
	if !dec.More() {
		return nil, errors.New("missing env")
	}
	key, err := dec.Token()
	if err != nil || key != "env" {
		return nil, errors.New("unknown response field")
	}
	tok, err = dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errors.New("env must be an object")
	}
	env := make(map[string]string)
	for dec.More() {
		nameToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := nameToken.(string)
		if !ok || !validEnvName(name) {
			return nil, errors.New("invalid env name")
		}
		if _, duplicate := env[name]; duplicate {
			return nil, errors.New("duplicate env name")
		}
		var value string
		if err := dec.Decode(&value); err != nil {
			return nil, errors.New("env value must be a string")
		}
		env[name] = value
	}
	if tok, err = dec.Token(); err != nil || tok != json.Delim('}') || dec.More() {
		return nil, errors.New("invalid env object")
	}
	if tok, err = dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errors.New("invalid response object")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("trailing response content")
	}
	return env, nil
}

func (h *operationHandler) write(w http.ResponseWriter, status int, outcome, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Credroute-Protocol", CredrouteProtocol)
	w.Header().Set("X-Credroute-Binding", h.op.BindingRevision)
	w.Header().Set("X-Credroute-Daemon", h.daemonRevision)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(operationResponse{
		Protocol: CredrouteProtocol, BindingRevision: h.op.BindingRevision,
		DaemonRevision: h.daemonRevision, Operation: h.op.Name, Outcome: outcome, Error: code,
	})
}
