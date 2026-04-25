package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/takezoh/credproxy/cmd/credproxyd/config"
	"github.com/takezoh/credproxy/cmd/credproxyd/providers/script"
	"github.com/takezoh/credproxy/pkg/credproxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "credproxyd:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "", "path to config.toml (required)")
	flag.Parse()

	if *cfgPath == "" {
		flag.Usage()
		return fmt.Errorf("--config is required")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	initLogger(cfg.LogLevel)
	slog.Info("credproxyd starting", "config", *cfgPath)

	var tokens []string
	if cfg.AuthTokensFile != "" {
		tokens, err = config.LoadTokens(cfg.AuthTokensFile)
		if err != nil {
			return err
		}
	}

	routes := buildRoutes(cfg.Routes)

	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            cfg.ListenTCP,
		ListenUnix:           cfg.ListenUnix,
		AuthTokens:           tokens,
		AllowUnauthenticated: cfg.ListenTCP == "" && len(tokens) == 0,
		Routes:               routes,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	slog.Info("credproxyd stopped")
	return nil
}

func buildRoutes(cfgRoutes []config.Route) []credproxy.Route {
	routes := make([]credproxy.Route, 0, len(cfgRoutes))
	for _, r := range cfgRoutes {
		timeout := time.Duration(r.HookTimeoutSec) * time.Second
		provider := script.New(
			trimPrefix(r.Path),
			r.CredentialCommand,
			r.RefreshCommand,
			timeout,
		)
		routes = append(routes, credproxy.Route{
			Path:             r.Path,
			Upstream:         r.Upstream,
			Provider:         provider,
			RefreshOnStatus:  r.RefreshOnStatus,
			StripInboundAuth: r.StripInboundAuth,
		})
	}
	return routes
}

func trimPrefix(path string) string {
	if len(path) > 0 && path[0] == '/' {
		return path[1:]
	}
	return path
}

func initLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}
