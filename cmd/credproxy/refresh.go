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
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	credential := fs.String("credential", "", "credential name to refresh")
	all := fs.Bool("all-preloaded", false, "refresh every preloaded credential")
	socket := fs.String("socket", "", "admin Unix socket path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return errors.New("refresh takes no positional arguments")
	}
	if *all == (*credential != "") {
		return errors.New("specify exactly one of --credential or --all-preloaded")
	}
	if *socket == "" {
		var err error
		*socket, err = admin.DefaultSocketPath()
		if err != nil {
			return err
		}
	}
	payload, err := json.Marshal(struct {
		Credential   string `json:"credential,omitempty"`
		AllPreloaded bool   `json:"all_preloaded,omitempty"`
	}{*credential, *all})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", *socket)
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
