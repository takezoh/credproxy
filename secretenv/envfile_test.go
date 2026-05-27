package secretenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestParseFile_basic(t *testing.T) {
	p := writeEnvFile(t, `
# comment
SECRET_KEY=op://vault/item/secret
API_TOKEN=op://vault/item/token
`)
	entries, err := ParseFile(p)
	require.NoError(t, err)
	assert.Equal(t, []Entry{
		{Name: "SECRET_KEY", Ref: "op://vault/item/secret"},
		{Name: "API_TOKEN", Ref: "op://vault/item/token"},
	}, entries)
}

func TestParseFile_skips(t *testing.T) {
	p := writeEnvFile(t, `
# full-line comment
  # indented comment

NO_EQUALS
=EMPTY_NAME
KEY=
`)
	entries, err := ParseFile(p)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParseFile_equalsInRef(t *testing.T) {
	p := writeEnvFile(t, "TOKEN=base64+value==")
	entries, err := ParseFile(p)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "base64+value==", entries[0].Ref)
}

func TestParseFile_notFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/path.env")
	assert.ErrorContains(t, err, "secretenv: open")
}
