//go:build !windows

package onepassword

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func readProtectedToken(path string) (string, error) {
	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("stat protected directory %s: %w", dir, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByUser(info) {
			return "", fmt.Errorf("protected directory identity invalid: %s", dir)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("stat protected token: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByUser(info) {
		return "", fmt.Errorf("protected token identity invalid: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read protected token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	clear(raw)
	if token == "" {
		return "", fmt.Errorf("protected token is empty")
	}
	return token, nil
}

func ownedByUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
