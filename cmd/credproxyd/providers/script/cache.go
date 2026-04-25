package script

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/takezoh/credproxy/pkg/credproxy"
)

type cacheEntry struct {
	inj     *credproxy.Injection
	expires time.Time
}

// ttlCache is a concurrency-safe single-entry cache with singleflight de-duplication.
// The "single-entry" design mirrors the original: one Provider == one route == one credential.
type ttlCache struct {
	sf singleflight.Group
	mu sync.Mutex
	e  *cacheEntry
}

// get returns the cached injection if it has not expired, along with true.
func (c *ttlCache) get(now time.Time) (*credproxy.Injection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.e != nil && now.Before(c.e.expires) {
		return c.e.inj, true
	}
	return nil, false
}

// set stores an injection with the given expiry time.
func (c *ttlCache) set(inj *credproxy.Injection, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.e = &cacheEntry{inj: inj, expires: expires}
}

// invalidate clears the cached entry.
func (c *ttlCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.e = nil
}

// do invokes fn via singleflight: concurrent callers with the same key share one result.
// If fn returns a non-zero expiry the result is cached.
func (c *ttlCache) do(key string, fn func() (*credproxy.Injection, time.Time, error)) (*credproxy.Injection, error) {
	type result struct {
		inj     *credproxy.Injection
		expires time.Time
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		inj, expires, err := fn()
		if err != nil {
			return nil, err
		}
		if !expires.IsZero() {
			c.set(inj, expires)
		}
		return result{inj: inj, expires: expires}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(result).inj, nil
}
