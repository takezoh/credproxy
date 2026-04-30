package awssso

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	credproxylib "github.com/takezoh/credproxy/credproxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeAWSScript = `#!/bin/sh
profile=""
i=1
while [ $i -le $# ]; do
  eval "arg=\${$i}"
  if [ "$arg" = "--profile" ]; then
    i=$((i+1))
    eval "profile=\${$i}"
  fi
  i=$((i+1))
done
case "$1 $2 $3 $4" in
  "configure export-credentials --format process")
    if [ -z "$profile" ]; then profile="default"; fi
    echo "{\"Version\":1,\"AccessKeyId\":\"ID-$profile\",\"SecretAccessKey\":\"SECRET\",\"SessionToken\":\"TOKEN\",\"Expiration\":\"2099-01-01T00:00:00Z\"}"
    ;;
  *)
    echo "unknown args: $*" >&2
    exit 1
    ;;
esac
`

func withFakeAWS(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	awsPath := filepath.Join(dir, "aws")
	err := os.WriteFile(awsPath, []byte(fakeAWSScript), 0o755)
	require.NoError(t, err)
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+origPath)
}

func req(projectID, path string) credproxylib.Request {
	r := credproxylib.Request{Path: path}
	if projectID != "" {
		r.Metadata = map[string]string{"token_id": projectID}
	}
	return r
}

func TestGet_DefaultProfile(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"default"})
	inj, err := p.Get(context.Background(), req("proj-A", "/"))
	require.NoError(t, err)
	require.NotNil(t, inj)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, 1, creds.Version)
	assert.Equal(t, "ID-default", creds.AccessKeyId)
	assert.Equal(t, "TOKEN", creds.SessionToken)
}

func TestGet_NamedProfile(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	inj, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)
}

func TestGet_PerProfileCacheIsolation(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master", "general"})
	injMaster, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)
	injGeneral, err := p.Get(context.Background(), req("proj-A", "/general"))
	require.NoError(t, err)

	var cm, cg processCredentials
	require.NoError(t, json.Unmarshal(injMaster.BodyReplace, &cm))
	require.NoError(t, json.Unmarshal(injGeneral.BodyReplace, &cg))
	assert.Equal(t, "ID-master", cm.AccessKeyId)
	assert.Equal(t, "ID-general", cg.AccessKeyId)
}

func TestCache_ReusesWithinMargin(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	inj1, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)

	t.Setenv("PATH", "")

	inj2, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)
	assert.Equal(t, inj1.BodyReplace, inj2.BodyReplace)
}

func TestRefresh_ClearsCacheAndRefetches(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	_, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)
	p.mu.Lock()
	assert.NotNil(t, p.cache["master"])
	p.mu.Unlock()

	inj, err := p.Refresh(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)

	p.mu.Lock()
	assert.NotNil(t, p.cache["master"])
	p.mu.Unlock()
}

func TestCache_Expiry(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	expiredBody, _ := json.Marshal(processCredentials{Version: 1, AccessKeyId: "EXPIRED"})
	p.mu.Lock()
	p.cache["master"] = &cachedCreds{body: expiredBody, expires: time.Now().Add(-1 * time.Second)}
	p.mu.Unlock()

	inj, err := p.Get(context.Background(), req("proj-A", "/master"))
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)
}

func TestProfileFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/", ""},
		{"", ""},
		{"/master", "master"},
		{"/general", "general"},
		{"/default", ""},
	}
	for _, tc := range cases {
		got := profileFromPath(tc.path)
		assert.Equal(t, tc.want, got, "path=%q", tc.path)
	}
}

func TestContainerEnv_keys(t *testing.T) {
	env := ContainerEnv("mytoken", "/run/credproxy.sock")
	assert.Equal(t, "mytoken", env[EnvKeyToken])
	assert.Equal(t, "/run/credproxy.sock", env[EnvKeySock])
}

func TestGet_RejectUnlistedProfile(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	_, err := p.Get(context.Background(), req("proj-A", "/general"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "general")

	p.mu.Lock()
	_, cached := p.cache["general"]
	p.mu.Unlock()
	assert.False(t, cached)
}

func TestGet_RejectDefaultWhenNotListed(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	_, err := p.Get(context.Background(), req("proj-A", "/"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default")
}

func TestGet_UnknownProject(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	_, err := p.Get(context.Background(), req("proj-B", "/master"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proj-B")
}

func TestGet_ProjectIsolation(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"A-admin"})
	p.SetAllowedProfiles("proj-B", []string{"B-readonly"})

	_, err := p.Get(context.Background(), req("proj-B", "/A-admin"))
	require.Error(t, err, "proj-B must not access proj-A profile")

	_, err = p.Get(context.Background(), req("proj-A", "/B-readonly"))
	require.Error(t, err, "proj-A must not access proj-B profile")

	_, err = p.Get(context.Background(), req("proj-A", "/A-admin"))
	require.NoError(t, err)
	_, err = p.Get(context.Background(), req("proj-B", "/B-readonly"))
	require.NoError(t, err)
}

func TestSetAllowedProfiles_Replace(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"old-profile"})
	p.SetAllowedProfiles("proj-A", []string{"new-profile"})

	_, err := p.Get(context.Background(), req("proj-A", "/old-profile"))
	require.Error(t, err, "old-profile must be removed after replace")

	_, err = p.Get(context.Background(), req("proj-A", "/new-profile"))
	require.NoError(t, err)
}

func TestGet_NoMetadata(t *testing.T) {
	withFakeAWS(t)

	p := New()
	p.SetAllowedProfiles("proj-A", []string{"master"})
	_, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.Error(t, err)
}

func TestParseExpiration(t *testing.T) {
	assert.True(t, parseExpiration("").IsZero())
	assert.True(t, parseExpiration("not-a-date").IsZero())

	expected := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, parseExpiration("2099-01-01T00:00:00Z"))
}
