package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	credrouteProtocol    = "credroute/v1"
	ctxBindingRevision   = "ctx-sync/2"
	clientRevision       = "client/1"
	maxOperationResponse = 64 << 10
)

type operationRequest struct {
	Protocol        string   `json:"protocol"`
	BindingRevision string   `json:"binding_revision"`
	DaemonRevision  string   `json:"daemon_revision"`
	Arguments       []string `json:"arguments"`
}

type operationResult struct {
	Protocol        string `json:"protocol"`
	BindingRevision string `json:"binding_revision"`
	DaemonRevision  string `json:"daemon_revision"`
	Operation       string `json:"operation"`
	Outcome         string `json:"outcome"`
	Error           string `json:"error,omitempty"`
}

func operationCmd(args []string) error {
	return runOperationCmd(args, os.Stdout, os.Stderr)
}

func runOperationCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("operation", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", "", "fixed credproxyd Unix socket path (required)")
	route := fs.String("route", "", "closed operation name (required)")
	binding := fs.String("binding-revision", "", "required operation binding revision")
	daemon := fs.String("daemon-revision", "", "required installed daemon revision")
	timeoutSec := fs.Int("timeout-sec", 310, "request deadline in seconds")

	flagArgs, operationArgs := splitAtDashDash(args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if !filepath.IsAbs(*socket) || filepath.Clean(*socket) != *socket || !validRouteName(*route) || *binding == "" || *daemon == "" {
		return fmt.Errorf("--socket, --route, --binding-revision, and --daemon-revision are required")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("operation arguments require -- separator")
	}
	body, err := json.Marshal(operationRequest{
		Protocol: credrouteProtocol, BindingRevision: *binding,
		DaemonRevision: *daemon, Arguments: operationArgs,
	})
	if err != nil {
		return fmt.Errorf("operation request: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	resp, err := brokerPost(ctx, *socket, "v1/operations/"+*route, body, time.Duration(*timeoutSec)*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var result operationResult
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxOperationResponse))
	if err := dec.Decode(&result); err != nil {
		return fmt.Errorf("broker operation response invalid")
	}
	if result.Protocol != credrouteProtocol || result.BindingRevision != *binding || result.DaemonRevision != *daemon || result.Operation != *route {
		return fmt.Errorf("broker operation response binding mismatch")
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || result.Outcome != "success" {
		if result.Error == "" {
			return fmt.Errorf("closed operation failed")
		}
		return fmt.Errorf("closed operation failed: %s", result.Error)
	}
	return nil
}

func brokerPost(ctx context.Context, socketPath, path string, body []byte, timeout time.Duration) (*http.Response, error) {
	client := brokerHTTPClient(socketPath, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://credproxyd/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("broker request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker %s: %w", socketPath, err)
	}
	return resp, nil
}

func credrouteVersion() string {
	return credrouteProtocol + " " + ctxBindingRevision + " " + clientRevision
}
