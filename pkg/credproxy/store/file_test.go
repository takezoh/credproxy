package store_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/takezoh/credproxy/pkg/credproxy/store"
)

func TestFileStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	s := store.NewFileStore(dir, 0)

	data := []byte(`{"token":"abc"}`)
	if err := s.Save(context.Background(), "creds", data); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load(context.Background(), "creds")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Load = %q, want %q", got, data)
	}
}

func TestFileStore_Load_missing(t *testing.T) {
	dir := t.TempDir()
	s := store.NewFileStore(dir, 0)
	_, err := s.Load(context.Background(), "nokey")
	if err == nil {
		t.Error("expected error for missing key")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want os.ErrNotExist, got %v", err)
	}
}

func TestFileStore_Save_modeEnforced(t *testing.T) {
	dir := t.TempDir()
	s := store.NewFileStore(dir, 0)
	if err := s.Save(context.Background(), "creds", []byte("data")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(dir + "/creds")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestFileStore_Save_writeError(t *testing.T) {
	dir := t.TempDir()
	s := store.NewFileStore(dir, 0)
	// Remove write permission so WriteFile fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	err := s.Save(context.Background(), "key", []byte("data"))
	if err == nil {
		t.Error("expected error writing to read-only directory")
	}
}

func TestFileStore_Save_atomic(t *testing.T) {
	dir := t.TempDir()
	s := store.NewFileStore(dir, 0)
	// Write twice — second write must succeed (atomic rename not blocked by existing file).
	for i := 0; i < 2; i++ {
		if err := s.Save(context.Background(), "k", []byte("v")); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}
}
