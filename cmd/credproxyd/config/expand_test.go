package config

import (
	"testing"
)

func TestExpand_envVars(t *testing.T) {
	e := envFuncs{
		getenv: func(s string) string {
			return map[string]string{
				"127.0.0.1:${PORT}": "127.0.0.1:9999",
			}[s]
		},
		home: "/home/user",
	}
	c := Config{ListenTCP: "127.0.0.1:${PORT}"}
	got, err := expand(c, e)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got.ListenTCP != "127.0.0.1:9999" {
		t.Errorf("ListenTCP = %q, want 127.0.0.1:9999", got.ListenTCP)
	}
}

func TestExpand_tildeUnix(t *testing.T) {
	e := envFuncs{getenv: func(s string) string { return s }, home: "/home/user"}
	c := Config{ListenTCP: "127.0.0.1:9787", ListenUnix: "~/tmp/sock"}
	got, err := expand(c, e)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got.ListenUnix != "/home/user/tmp/sock" {
		t.Errorf("ListenUnix = %q, want /home/user/tmp/sock", got.ListenUnix)
	}
}

func TestExpand_tildeNoHome(t *testing.T) {
	e := envFuncs{getenv: func(s string) string { return s }, home: ""}
	c := Config{ListenTCP: "127.0.0.1:9787", ListenUnix: "~/tmp/sock"}
	_, err := expand(c, e)
	if err == nil {
		t.Error("expected error when home is empty and path starts with ~/")
	}
}

func TestExpand_defaultTimeout(t *testing.T) {
	e := envFuncs{getenv: func(s string) string { return s }, home: "/home/user"}
	c := Config{
		ListenTCP: "127.0.0.1:9787",
		Routes:    []Route{{Path: "/x", Upstream: "https://example.com", HookTimeoutSec: 0}},
	}
	got, err := expand(c, e)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got.Routes[0].HookTimeoutSec != 10 {
		t.Errorf("HookTimeoutSec = %d, want 10", got.Routes[0].HookTimeoutSec)
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		p, home string
		want    string
		wantErr bool
	}{
		{"~/foo/bar", "/home/u", "/home/u/foo/bar", false},
		{"/abs/path", "/home/u", "/abs/path", false},
		{"relative", "/home/u", "relative", false},
		{"~/foo", "", "", true},
		{"", "/home/u", "", false},
	}
	for _, tt := range tests {
		got, err := expandPath(tt.p, tt.home)
		if (err != nil) != tt.wantErr {
			t.Errorf("expandPath(%q, %q) err = %v, wantErr %v", tt.p, tt.home, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("expandPath(%q, %q) = %q, want %q", tt.p, tt.home, got, tt.want)
		}
	}
}
