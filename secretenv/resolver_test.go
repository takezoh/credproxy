package secretenv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHook struct {
	vals map[string]string
}

func (s *stubHook) Resolve(_ context.Context, ref string) (string, error) {
	v, ok := s.vals[ref]
	if !ok {
		return "", &stubErr{ref: ref}
	}
	return v, nil
}

// emptyHook always returns ("", nil) to simulate a misconfigured backend.
type emptyHook struct{}

func (emptyHook) Resolve(_ context.Context, _ string) (string, error) { return "", nil }

type stubErr struct{ ref string }

func (e *stubErr) Error() string { return "stub: unknown ref " + e.ref }

func TestResolver_ResolveFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(p, []byte("FOO=ref://foo\nBAR=ref://bar\n"), 0o600))

	r := NewResolver(&stubHook{vals: map[string]string{
		"ref://foo": "foo-secret",
		"ref://bar": "bar-secret",
	}})
	got, err := r.ResolveFile(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOO": "foo-secret", "BAR": "bar-secret"}, got)
}

func TestResolver_ResolveFile_hookError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(p, []byte("BAD=ref://unknown\n"), 0o600))

	r := NewResolver(&stubHook{vals: map[string]string{}})
	_, err := r.ResolveFile(context.Background(), p)
	assert.ErrorContains(t, err, "BAD")
	assert.ErrorContains(t, err, "ref://unknown")
}

func TestResolver_ResolveFile_empty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.env")
	require.NoError(t, os.WriteFile(p, []byte("# only comments\n"), 0o600))

	r := NewResolver(&stubHook{vals: map[string]string{}})
	got, err := r.ResolveFile(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolver_ResolveFile_emptyValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(p, []byte("SECRET=ref://thing\n"), 0o600))

	r := NewResolver(emptyHook{})
	_, err := r.ResolveFile(context.Background(), p)
	assert.ErrorContains(t, err, "SECRET")
	assert.ErrorContains(t, err, "empty value")
}
