package container

import (
	"crypto/sha256"
	"fmt"
)

// ProjectRunHash returns the per-project run-directory name (6 bytes → 12 hex chars).
func ProjectRunHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("%x", h[:6])
}
