package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/takezoh/credproxy/cmd/credproxyd/admin"
)

func refreshCmd(args []string) error {
	options, err := parseRefreshOptions(args)
	if err != nil {
		return err
	}
	endpoint, expectedExe, err := adminTarget(options.socket, options.serverExecutable)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(refreshPayload{Credential: options.credential, AllPreloaded: options.all})
	if err != nil {
		return err
	}
	return sendRefreshRequest(endpoint, expectedExe, payload)
}

type refreshOptions struct {
	credential       string
	all              bool
	socket           string
	serverExecutable string
}

type refreshPayload struct {
	Credential   string `json:"credential,omitempty"`
	AllPreloaded bool   `json:"all_preloaded,omitempty"`
}

func parseRefreshOptions(args []string) (refreshOptions, error) {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	credential := fs.String("credential", "", "credential name to refresh")
	all := fs.Bool("all-preloaded", false, "refresh every preloaded credential")
	socket := fs.String("socket", "", "admin Unix socket or named-pipe endpoint")
	serverExecutable := fs.String("server-executable", "", "expected protected credproxyd executable on Windows")
	if err := fs.Parse(args); err != nil {
		return refreshOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return refreshOptions{}, errors.New("refresh takes no positional arguments")
	}
	if *all == (*credential != "") {
		return refreshOptions{}, errors.New("specify exactly one of --credential or --all-preloaded")
	}
	return refreshOptions{
		credential: *credential, all: *all, socket: *socket,
		serverExecutable: *serverExecutable,
	}, nil
}

func adminTarget(socket, serverExecutable string) (string, string, error) {
	if socket == "" {
		var err error
		socket, err = admin.DefaultSocketPath()
		if err != nil {
			return "", "", err
		}
	}
	expectedExe, err := expectedServerExecutable(serverExecutable)
	if err != nil {
		return "", "", fmt.Errorf("expected server executable: %w", err)
	}
	return socket, expectedExe, nil
}

func sendRefreshRequest(socket, expectedExe string, payload []byte) error {
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return brokerDialer(ctx, socket, expectedExe)
		},
	}}
	req, err := http.NewRequest(http.MethodPost, "http://unix/refresh", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		detail, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
		if err != nil {
			return fmt.Errorf("refresh failed (%s): read response: %w", resp.Status, err)
		}
		return fmt.Errorf("refresh failed (%s): %s", resp.Status, bytes.TrimSpace(detail))
	}
	fmt.Fprintln(os.Stdout, "credential refreshed")
	return nil
}
