package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
)

// connectCmd relays raw bytes over one authenticated broker connection.
func connectCmd(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	endpoint := fs.String("socket", "", "broker Unix socket or Windows named-pipe endpoint")
	serverExecutable := fs.String("server-executable", "", "expected protected credproxyd executable on Windows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("connect takes no positional arguments")
	}
	if *endpoint == "" {
		var err error
		*endpoint, err = defaultBrokerEndpoint()
		if err != nil {
			return fmt.Errorf("broker endpoint: %w", err)
		}
	}
	expected, err := expectedServerExecutable(*serverExecutable)
	if err != nil {
		return fmt.Errorf("expected server executable: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), commandSignals()...)
	defer stop()
	conn, err := brokerDialer(ctx, *endpoint, expected)
	if err != nil {
		return fmt.Errorf("broker connection: %w", err)
	}
	defer conn.Close()
	return relayStdio(ctx, conn, os.Stdin, os.Stdout)
}

func relayStdio(ctx context.Context, conn net.Conn, input io.Reader, output io.Writer) error {
	results := make(chan relayResult, 2)
	go copyToConnection(conn, input, results)
	go copyFromConnection(conn, output, results)
	for {
		select {
		case result := <-results:
			if result.output {
				_ = conn.Close()
				if result.err != nil {
					return fmt.Errorf("copy broker stream to stdout: %w", result.err)
				}
				return nil
			}
			if result.err != nil {
				_ = conn.Close()
				return fmt.Errorf("copy stdin to broker stream: %w", result.err)
			}
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		}
	}
}

type relayResult struct {
	output bool
	err    error
}

func copyToConnection(conn net.Conn, input io.Reader, results chan<- relayResult) {
	_, err := io.Copy(conn, input)
	if err == nil {
		if halfCloser, ok := conn.(interface{ CloseWrite() error }); ok {
			err = halfCloser.CloseWrite()
		}
	}
	results <- relayResult{err: err}
}

func copyFromConnection(conn net.Conn, output io.Writer, results chan<- relayResult) {
	_, err := io.Copy(output, conn)
	results <- relayResult{output: true, err: err}
}
