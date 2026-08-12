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
	"github.com/takezoh/credproxy/credproxy"
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

	var rawTokens []config.Token
	if cfg.AuthTokensFile != "" {
		rawTokens, err = config.LoadTokens(cfg.AuthTokensFile)
		if err != nil {
			return err
		}
	}
	tokens := make([]credproxy.TokenAuth, len(rawTokens))
	ids := make([]string, len(rawTokens))
	for i, t := range rawTokens {
		id := t.ID
		if id == "" {
			id = fmt.Sprintf("token-%d", i)
		}
		tokens[i] = credproxy.TokenAuth{Token: t.Value, ID: id}
		ids[i] = id
	}
	if err := config.ValidateClientIDs(cfg.Routes, ids); err != nil {
		return err
	}

	routes := buildRoutes(cfg.Routes)
	operations, err := buildOperations(cfg.Operations)
	if err != nil {
		return err
	}

	srv, err := credproxy.New(credproxy.ServerConfig{
		ListenTCP:            cfg.ListenTCP,
		ListenUnix:           cfg.ListenUnix,
		AuthTokens:           tokens,
		AllowUnauthenticated: cfg.ListenTCP == "" && len(tokens) == 0,
		Routes:               routes,
		Operations:           operations,
		DaemonRevision:       cfg.DaemonRevision,
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

func buildOperations(cfgOperations []config.Operation) ([]credproxy.Operation, error) {
	operations := make([]credproxy.Operation, 0, len(cfgOperations))
	for _, op := range cfgOperations {
		arguments := make([]credproxy.OperationArgument, 0, len(op.Arguments))
		for _, arg := range op.Arguments {
			min, err := time.ParseDuration(arg.Min)
			if err != nil {
				return nil, fmt.Errorf("operation %s argument %s min: %w", op.Name, arg.Flag, err)
			}
			max, err := time.ParseDuration(arg.Max)
			if err != nil {
				return nil, fmt.Errorf("operation %s argument %s max: %w", op.Name, arg.Flag, err)
			}
			arguments = append(arguments, credproxy.OperationArgument{
				Flag: arg.Flag, ValueType: arg.Type, Min: min, Max: max,
			})
		}
		provider := script.New(op.Name, op.CredentialCommand, nil, time.Duration(op.HookTimeoutSec)*time.Second)
		operations = append(operations, credproxy.Operation{
			Name: op.Name, BindingRevision: op.BindingRevision,
			ExecutablePaths: op.ExecutablePaths, Subcommand: op.Subcommand,
			Arguments: arguments, Environment: op.Environment, FixedEnv: op.FixedEnv,
			PassEnv: op.PassEnv, Provider: provider,
			CredentialTimeout: time.Duration(op.HookTimeoutSec) * time.Second,
			MaxRuntime:        time.Duration(op.MaxRuntimeSec) * time.Second,
		})
	}
	return operations, nil
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
			AllowedClientIDs: r.AllowedClientIDs,
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
