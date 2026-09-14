// credproxy-host-broker-adapter serves the private Host Broker contract.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/takezoh/credproxy/internal/hostbrokeradapter"
)

func main() {
	configPath := flag.String("config", "", "owner-only adapter configuration")
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "credproxy-host-broker-adapter: --config is required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "credproxy-host-broker-adapter: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path string) error {
	cfg, err := hostbrokeradapter.LoadConfig(path)
	if err != nil {
		return err
	}
	application, err := hostbrokeradapter.NewApplication(ctx, cfg)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: cfg.Listen, Handler: application, ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile) }()
	// The listener must exist before registration because Host Broker performs a
	// TLS trust probe as part of RegisterInstance.
	registrationErrors := make(chan error, 1)
	go func() {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			registrationErrors <- nil
		case <-timer.C:
			registrationErrors <- hostbrokeradapter.MaintainRegistration(ctx, cfg)
		}
	}()
	select {
	case <-ctx.Done():
	case err = <-registrationErrors:
		if err != nil {
			return err
		}
	case err = <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
