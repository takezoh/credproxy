//go:build windows

package onepassword

import (
	"fmt"
	"io"
	"strings"

	"github.com/takezoh/credproxy/internal/winsecurefile"
)

func readProtectedToken(path string) (string, error) {
	file, _, err := winsecurefile.Open(path)
	if err != nil {
		return "", fmt.Errorf("validate protected 1Password token: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read protected token handle: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	clear(raw)
	if token == "" {
		return "", fmt.Errorf("protected token is empty")
	}
	return token, nil
}
