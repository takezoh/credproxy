// Package secretenv provides host-side secret reference resolution.
// It is deliberately independent of the credproxy HTTP proxy core and
// the container package: no broker, no token, no container knowledge.
package secretenv

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Entry is a name=ref pair from a secrets env-file.
// Ref is an opaque reference string interpreted by the Hook backend
// (e.g. "op://vault/item/field" for 1Password).
type Entry struct {
	Name string
	Ref  string
}

// ParseFile reads the env-file at path and returns all name=ref pairs.
// Lines starting with '#' are comments; lines without '=' are ignored;
// entries with empty name or ref are ignored.
// Values are taken verbatim — no shell quoting or expansion.
func ParseFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("secretenv: open %s: %w", path, err)
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		ref := strings.TrimSpace(line[idx+1:])
		if name == "" || ref == "" {
			continue
		}
		entries = append(entries, Entry{Name: name, Ref: ref})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("secretenv: read %s: %w", path, err)
	}
	return entries, nil
}
