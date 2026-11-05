package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// logOutput receives the JSON logs. On Windows credproxyd is built without a
// console, so --log-file is the only place its logs can go.
var logOutput io.Writer = os.Stderr

// maxLogBytes bounds the log file. When the file is larger at startup, it is
// moved to <path>.1 so that one previous generation is kept.
var maxLogBytes int64 = 10 << 20

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: l})))
}
