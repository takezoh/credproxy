package onepassword

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	sdk "github.com/1password/onepassword-sdk-go"
	"golang.org/x/sync/singleflight"
)

// Credential identifies a 1Password reference and its retention policy.
type Credential struct {
	Name    string
	Ref     string
	Preload bool
	TTL     time.Duration
}

type Resolver interface {
	Resolve(context.Context, string) (string, error)
}
type entry struct {
	value   string
	expires time.Time
	valid   bool
}

// Provider shares cached references across every route of a daemon instance.
type Provider struct {
	resolver Resolver
	specs    map[string]Credential
	mu       sync.RWMutex
	cache    map[string]entry
	group    singleflight.Group
	locks    map[string]*sync.Mutex
	now      func() time.Time
}

func New(resolver Resolver, specs []Credential) (*Provider, error) {
	if resolver == nil {
		return nil, fmt.Errorf("1Password resolver is nil")
	}
	p := &Provider{resolver: resolver, specs: make(map[string]Credential), cache: make(map[string]entry), locks: make(map[string]*sync.Mutex), now: time.Now}
	for _, spec := range specs {
		if spec.Name == "" || !strings.HasPrefix(spec.Ref, "op://") || (!spec.Preload && spec.TTL <= 0) {
			return nil, fmt.Errorf("invalid 1Password credential %q", spec.Name)
		}
		if _, exists := p.specs[spec.Name]; exists {
			return nil, fmt.Errorf("duplicate 1Password credential %q", spec.Name)
		}
		p.specs[spec.Name] = spec
		p.locks[spec.Name] = &sync.Mutex{}
	}
	return p, nil
}

// Preload resolves all pinned credentials before the listener opens.
func (p *Provider) Preload(ctx context.Context) error {
	values := make(map[string]entry)
	for name, spec := range p.specs {
		if !spec.Preload {
			continue
		}
		value, err := p.resolver.Resolve(ctx, spec.Ref)
		if err != nil {
			return fmt.Errorf("preload %s: %w", name, err)
		}
		if value == "" {
			return fmt.Errorf("preload %s: 1Password returned an empty value", name)
		}
		values[name] = entry{value: value, valid: true}
	}
	p.mu.Lock()
	for k, v := range values {
		p.cache[k] = v
	}
	p.mu.Unlock()
	return nil
}

// Get uses a pinned value or a TTL-bound value, resolving concurrent misses once.
func (p *Provider) Get(ctx context.Context, name string) (string, error) {
	spec, ok := p.specs[name]
	if !ok {
		return "", fmt.Errorf("unknown credential %q", name)
	}
	p.mu.RLock()
	cached := p.cache[name]
	p.mu.RUnlock()
	if cached.valid && (spec.Preload || p.now().Before(cached.expires)) {
		return cached.value, nil
	}
	value, err, _ := p.group.Do(name, func() (any, error) {
		p.locks[name].Lock()
		defer p.locks[name].Unlock()
		p.mu.RLock()
		cached := p.cache[name]
		p.mu.RUnlock()
		if cached.valid && (spec.Preload || p.now().Before(cached.expires)) {
			return cached.value, nil
		}
		return p.resolve(ctx, name, spec)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

// Refresh replaces only the named credential after successful resolution.
func (p *Provider) Refresh(ctx context.Context, name string) (string, error) {
	spec, ok := p.specs[name]
	if !ok {
		return "", fmt.Errorf("unknown credential %q", name)
	}
	p.locks[name].Lock()
	defer p.locks[name].Unlock()
	return p.resolve(ctx, name, spec)
}

// RefreshMany resolves the selected set before replacing any cached values.
// With allPreloaded, names are ignored and every pinned credential is selected.
func (p *Provider) RefreshMany(ctx context.Context, names []string, allPreloaded bool) error {
	selected, err := p.selectedNames(names, allPreloaded)
	if err != nil {
		return err
	}
	for _, name := range selected {
		p.locks[name].Lock()
	}
	defer func() {
		for i := len(selected) - 1; i >= 0; i-- {
			p.locks[selected[i]].Unlock()
		}
	}()
	next, err := p.resolveSelected(ctx, selected)
	if err != nil {
		return err
	}
	p.mu.Lock()
	for name, item := range next {
		p.cache[name] = item
	}
	p.mu.Unlock()
	return nil
}

func (p *Provider) selectedNames(names []string, allPreloaded bool) ([]string, error) {
	selected := append([]string(nil), names...)
	if allPreloaded {
		selected = nil
		for name, spec := range p.specs {
			if spec.Preload {
				selected = append(selected, name)
			}
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no credentials selected for refresh")
	}
	sort.Strings(selected)
	unique := selected[:0]
	for _, name := range selected {
		if len(unique) == 0 || name != unique[len(unique)-1] {
			unique = append(unique, name)
		}
	}
	selected = unique
	for _, name := range selected {
		if _, ok := p.specs[name]; !ok {
			return nil, fmt.Errorf("unknown credential %q", name)
		}
	}
	return selected, nil
}

func (p *Provider) resolveSelected(ctx context.Context, selected []string) (map[string]entry, error) {
	next := make(map[string]entry, len(selected))
	for _, name := range selected {
		spec := p.specs[name]
		value, err := p.resolver.Resolve(ctx, spec.Ref)
		if err != nil {
			return nil, fmt.Errorf("refresh %s: %w", name, err)
		}
		if value == "" {
			return nil, fmt.Errorf("refresh %s: 1Password returned an empty value", name)
		}
		item := entry{value: value, valid: true}
		if !spec.Preload {
			item.expires = p.now().Add(spec.TTL)
		}
		next[name] = item
	}
	return next, nil
}

func (p *Provider) resolve(ctx context.Context, name string, spec Credential) (string, error) {
	value, err := p.resolver.Resolve(ctx, spec.Ref)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	if value == "" {
		return "", fmt.Errorf("resolve %s: 1Password returned an empty value", name)
	}
	e := entry{value: value, valid: true}
	if !spec.Preload {
		e.expires = p.now().Add(spec.TTL)
	}
	p.mu.Lock()
	p.cache[name] = e
	p.mu.Unlock()
	return value, nil
}

// NewSDK opens the service account credential file once at daemon startup.
func NewSDK(ctx context.Context, tokenPath string) (Resolver, error) {
	token, err := readProtectedToken(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("read protected 1Password token: %w", err)
	}
	client, err := sdk.NewClient(ctx, sdk.WithServiceAccountToken(token), sdk.WithIntegrationInfo("credproxyd", "v1"))
	token = ""
	if err != nil {
		return nil, fmt.Errorf("initialize 1Password SDK: %w", err)
	}
	return newSDKResolver(client), nil
}

// sdkResolver retains the parent SDK client: SecretsAPI alone does not keep
// the client's finalizer from releasing the underlying native client ID.
type sdkResolver struct {
	client *sdk.Client
}

func newSDKResolver(client *sdk.Client) Resolver {
	return &sdkResolver{client: client}
}

func (r *sdkResolver) Resolve(ctx context.Context, ref string) (string, error) {
	defer runtime.KeepAlive(r.client)
	return r.client.Secrets().Resolve(ctx, ref)
}

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
