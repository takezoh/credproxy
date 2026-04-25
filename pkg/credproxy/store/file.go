package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileStore persists each key as a separate file under a directory.
// Writes are atomic (tmp file + rename). Mode 0600 is enforced.
type FileStore struct {
	dir  string
	mode os.FileMode
}

// NewFileStore returns a Store that reads/writes files in dir.
// mode is applied to each saved file (0600 if 0).
func NewFileStore(dir string, mode os.FileMode) *FileStore {
	if mode == 0 {
		mode = 0o600
	}
	return &FileStore{dir: dir, mode: mode}
}

func (s *FileStore) Load(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		return nil, fmt.Errorf("store: load %s: %w", key, err)
	}
	return data, nil
}

func (s *FileStore) Save(_ context.Context, key string, data []byte) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", s.dir, err)
	}
	dst := s.path(key)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, s.mode); err != nil {
		return fmt.Errorf("store: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("store: rename %s: %w", tmp, err)
	}
	return nil
}

func (s *FileStore) path(key string) string {
	return filepath.Join(s.dir, key)
}
