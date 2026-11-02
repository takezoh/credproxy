//go:build windows

package admin

import (
	"testing"

	"github.com/takezoh/credproxy/internal/nativeipc"
)

func TestDefaultSocketPathUsesCurrentSID(t *testing.T) {
	got, err := DefaultSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := nativeipc.DefaultPipePath("admin")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("admin endpoint = %q, want %q", got, want)
	}
}
