package awssso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	credproxylib "github.com/takezoh/credproxy/credproxy"
)

// RoutePath is the proxy path prefix served by this provider.
const RoutePath = "/aws-credentials"

// processCredentials is the JSON format required by credential_process.
type processCredentials struct {
	Version         int    `json:"Version"`
	AccessKeyId     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken,omitempty"`
	Expiration      string `json:"Expiration,omitempty"`
}

type cachedCreds struct {
	body    []byte
	expires time.Time
}

// Provider implements credproxylib.Provider for AWS SSO.
// It shells out to the aws CLI to obtain temporary credentials per profile and
// serves them as a credential_process-compatible JSON body (BodyReplace).
// Credentials are cached per profile with a 60-second early-refresh margin.
//
// Access is gated per project: only profiles registered for the requesting
// project (identified by Request.Metadata["token_id"]) are served.
type Provider struct {
	mu      sync.Mutex
	cache   map[string]*cachedCreds        // keyed by profile name ("" = default)
	allowed map[string]map[string]struct{} // projectID → set of allowed profile keys
}

const refreshMargin = 60 * time.Second

// New creates a Provider with an empty per-project allowlist.
func New() *Provider {
	return &Provider{
		cache:   make(map[string]*cachedCreds),
		allowed: make(map[string]map[string]struct{}),
	}
}

// SetAllowedProfiles replaces the profile allowlist for projectID.
func (p *Provider) SetAllowedProfiles(projectID string, profiles []string) {
	set := make(map[string]struct{}, len(profiles))
	for _, name := range profiles {
		set[allowKey(name)] = struct{}{}
	}
	p.mu.Lock()
	p.allowed[projectID] = set
	p.mu.Unlock()
}

func allowKey(profile string) string {
	if profile == "" {
		return "default"
	}
	return profile
}

func (p *Provider) Get(ctx context.Context, req credproxylib.Request) (*credproxylib.Injection, error) {
	projectID := req.Metadata["token_id"]
	profile := profileFromPath(req.Path)
	key := allowKey(profile)

	p.mu.Lock()
	set, hasProject := p.allowed[projectID]
	if !hasProject {
		p.mu.Unlock()
		return nil, fmt.Errorf("awssso: no allowlist for project %q", projectID)
	}
	_, ok := set[key]
	if c := p.cache[profile]; ok && c != nil && time.Now().Add(refreshMargin).Before(c.expires) {
		body := c.body
		p.mu.Unlock()
		return &credproxylib.Injection{BodyReplace: body}, nil
	}
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("awssso: profile %q not allowed for project %q", key, projectID)
	}
	return p.fetch(ctx, profile)
}

func (p *Provider) Refresh(ctx context.Context, req credproxylib.Request) (*credproxylib.Injection, error) {
	projectID := req.Metadata["token_id"]
	profile := profileFromPath(req.Path)
	key := allowKey(profile)

	p.mu.Lock()
	set, hasProject := p.allowed[projectID]
	var allowed bool
	if hasProject {
		if _, allowed = set[key]; allowed {
			delete(p.cache, profile)
		}
	}
	p.mu.Unlock()

	if !hasProject {
		return nil, fmt.Errorf("awssso: no allowlist for project %q", projectID)
	}
	if !allowed {
		return nil, fmt.Errorf("awssso: profile %q not allowed for project %q", key, projectID)
	}
	return p.fetch(ctx, profile)
}

func (p *Provider) fetch(ctx context.Context, profile string) (*credproxylib.Injection, error) {
	creds, expires, err := obtainCredentials(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("awssso: %w", err)
	}

	body, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("awssso: marshal: %w", err)
	}

	p.mu.Lock()
	p.cache[profile] = &cachedCreds{body: body, expires: expires}
	p.mu.Unlock()

	return &credproxylib.Injection{BodyReplace: body, ExpiresAt: expires}, nil
}

func profileFromPath(path string) string {
	p := strings.TrimPrefix(path, "/")
	if p == "default" {
		return ""
	}
	return p
}

func obtainCredentials(ctx context.Context, profile string) (processCredentials, time.Time, error) {
	if creds, exp, err := exportCredentials(ctx, profile); err == nil {
		return creds, exp, nil
	}
	return ssoCredentials(ctx)
}

func exportCredentials(ctx context.Context, profile string) (processCredentials, time.Time, error) {
	args := []string{"configure", "export-credentials", "--format", "process"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", args...)
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return processCredentials{}, time.Time{}, err
	}

	var raw struct {
		AccessKeyId     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		Expiration      string `json:"Expiration"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return processCredentials{}, time.Time{}, err
	}
	if raw.AccessKeyId == "" {
		return processCredentials{}, time.Time{}, fmt.Errorf("export-credentials: no AccessKeyId")
	}

	return processCredentials{
		Version:         1,
		AccessKeyId:     raw.AccessKeyId,
		SecretAccessKey: raw.SecretAccessKey,
		SessionToken:    raw.SessionToken,
		Expiration:      raw.Expiration,
	}, parseExpiration(raw.Expiration), nil
}

func ssoCredentials(ctx context.Context) (processCredentials, time.Time, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return processCredentials{}, time.Time{}, fmt.Errorf("sso cache dir: %w", err)
	}

	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			continue
		}

		var token struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   string `json:"expiresAt"`
			AccountId   string `json:"accountId"`
			RoleName    string `json:"roleName"`
		}
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		if token.AccessToken == "" || token.AccountId == "" || token.RoleName == "" {
			continue
		}
		exp := parseExpiration(token.ExpiresAt)
		if !exp.IsZero() && exp.Before(now) {
			continue
		}

		creds, expires, err := getRoleCredentials(ctx, token.AccountId, token.RoleName, token.AccessToken)
		if err != nil {
			continue
		}
		return creds, expires, nil
	}

	return processCredentials{}, time.Time{}, fmt.Errorf("no valid SSO session found; run `aws sso login`")
}

func getRoleCredentials(ctx context.Context, accountID, roleName, accessToken string) (processCredentials, time.Time, error) {
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", "sso", "get-role-credentials",
		"--account-id", accountID,
		"--role-name", roleName,
		"--access-token", accessToken,
		"--output", "json",
	)
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return processCredentials{}, time.Time{}, err
	}

	var result struct {
		RoleCredentials struct {
			AccessKeyId     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			SessionToken    string `json:"sessionToken"`
			Expiration      int64  `json:"expiration"`
		} `json:"roleCredentials"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return processCredentials{}, time.Time{}, err
	}
	rc := result.RoleCredentials
	if rc.AccessKeyId == "" {
		return processCredentials{}, time.Time{}, fmt.Errorf("get-role-credentials: no AccessKeyId")
	}

	var expires time.Time
	var expStr string
	if rc.Expiration > 0 {
		expires = time.UnixMilli(rc.Expiration)
		expStr = expires.UTC().Format(time.RFC3339)
	}

	return processCredentials{
		Version:         1,
		AccessKeyId:     rc.AccessKeyId,
		SecretAccessKey: rc.SecretAccessKey,
		SessionToken:    rc.SessionToken,
		Expiration:      expStr,
	}, expires, nil
}

func parseExpiration(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
