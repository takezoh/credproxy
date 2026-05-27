package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/takezoh/credproxy/secretenv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "credproxy:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: credproxy <command>\ncommands:\n  run      resolve env-file refs and exec a command\n  resolve  resolve env-file refs and print JSON env")
	}
	switch args[0] {
	case "run":
		return runCmd(args[1:])
	case "resolve":
		return resolveCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file path (default: $CREDPROXY_CONFIG or ~/.config/credproxy/config.toml)")
	envFile := fs.String("env-file", "", "env-file with name=ref declarations (required)")

	flagArgs, cmdArgs := splitAtDashDash(args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *envFile == "" {
		return fmt.Errorf("--env-file is required")
	}
	if len(cmdArgs) == 0 {
		return fmt.Errorf("command is required after --")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolved, err := resolveEnvFile(ctx, *configPath, *envFile)
	if err != nil {
		return err
	}

	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("lookup %s: %w", cmdArgs[0], err)
	}
	return syscall.Exec(bin, cmdArgs, mergeEnv(os.Environ(), resolved))
}

func resolveCmd(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file path (default: $CREDPROXY_CONFIG or ~/.config/credproxy/config.toml)")
	envFile := fs.String("env-file", "", "env-file with name=ref declarations (required)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *envFile == "" {
		return fmt.Errorf("--env-file is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolved, err := resolveEnvFile(ctx, *configPath, *envFile)
	if err != nil {
		return err
	}

	// Output only the resolved map — never os.Environ(). This is the security
	// boundary: only env-file-declared secrets cross to the caller (e.g. roost broker).
	return json.NewEncoder(os.Stdout).Encode(struct {
		Env map[string]string `json:"env"`
	}{Env: resolved})
}

// resolveEnvFile loads config, builds a ScriptHook, and resolves the env-file.
// configPath may be empty, in which case defaultConfigPath() is used.
func resolveEnvFile(ctx context.Context, configPath, envFile string) (map[string]string, error) {
	if configPath == "" {
		configPath = defaultConfigPath()
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	hook := secretenv.NewScriptHook(cfg.Hook, cfg.hookTimeout())
	resolver := secretenv.NewResolver(hook)
	return resolver.ResolveFile(ctx, envFile)
}

// splitAtDashDash splits args at the first "--" separator.
func splitAtDashDash(args []string) (before, after []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// mergeEnv returns base with resolved secrets overriding existing keys.
func mergeEnv(base []string, resolved map[string]string) []string {
	if len(resolved) == 0 {
		return base
	}
	override := make(map[string]bool, len(resolved))
	for k := range resolved {
		override[k] = true
	}
	out := make([]string, 0, len(base)+len(resolved))
	for _, e := range base {
		idx := strings.IndexByte(e, '=')
		if idx >= 0 && override[e[:idx]] {
			continue
		}
		out = append(out, e)
	}
	for k, v := range resolved {
		out = append(out, k+"="+v)
	}
	return out
}
