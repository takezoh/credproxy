package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLogFileCreatesParentAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "credproxyd.log")
	for _, line := range []string{"first\n", "second\n"} {
		f, err := openLogFile(path)
		if err != nil {
			t.Fatalf("openLogFile: %v", err)
		}
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Fatalf("log was not appended: %q", got)
	}
}

func TestOpenLogFileKeepsOnePreviousGeneration(t *testing.T) {
	previous := maxLogBytes
	maxLogBytes = 4
	t.Cleanup(func() { maxLogBytes = previous })
	path := filepath.Join(t.TempDir(), "credproxyd.log")
	if err := os.WriteFile(path, []byte("older log"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openLogFile(path)
	if err != nil {
		t.Fatalf("openLogFile: %v", err)
	}
	f.Close()
	rotated, err := os.ReadFile(path + ".1")
	if err != nil || string(rotated) != "older log" {
		t.Fatalf("previous generation was not kept: %q, %v", rotated, err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("current log was not started fresh: %v", err)
	}
}

func TestInitLoggerWritesJSONToLogOutput(t *testing.T) {
	previousOutput, previousLogger := logOutput, slog.Default()
	t.Cleanup(func() {
		logOutput = previousOutput
		slog.SetDefault(previousLogger)
	})
	var buffer bytes.Buffer
	logOutput = &buffer
	initLogger("warn")
	slog.Info("hidden at warn level")
	slog.Warn("written", "route", "example")
	var record map[string]any
	if err := json.NewDecoder(io.Reader(&buffer)).Decode(&record); err != nil {
		t.Fatalf("log output was not JSON: %v (%q)", err, buffer.String())
	}
	if record["msg"] != "written" || record["route"] != "example" {
		t.Fatalf("unexpected log record: %v", record)
	}
}
