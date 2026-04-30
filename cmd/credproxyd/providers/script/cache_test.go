package script

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/credproxy/credproxy"
)

func TestTTLCache_getSetInvalidate(t *testing.T) {
	var c ttlCache
	now := time.Now()

	// Empty cache: miss.
	if _, ok := c.get(now); ok {
		t.Error("expected miss on empty cache")
	}

	inj := &credproxy.Injection{Headers: map[string]string{"X-Test": "1"}}
	c.set(inj, now.Add(time.Minute))

	// Hit before expiry.
	got, ok := c.get(now)
	if !ok || got != inj {
		t.Errorf("expected cache hit, got ok=%v inj=%v", ok, got)
	}

	// Miss after expiry.
	if _, ok := c.get(now.Add(2 * time.Minute)); ok {
		t.Error("expected cache miss after expiry")
	}

	// Invalidate clears entry.
	c.set(inj, now.Add(time.Hour))
	c.invalidate()
	if _, ok := c.get(now); ok {
		t.Error("expected miss after invalidate")
	}
}

func TestTTLCache_do_singleflight(t *testing.T) {
	var c ttlCache
	var callCount atomic.Int32

	var wg sync.WaitGroup
	const goroutines = 50
	results := make([]*credproxy.Injection, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = c.do("key", func() (*credproxy.Injection, time.Time, error) {
				callCount.Add(1)
				time.Sleep(5 * time.Millisecond) // simulate subprocess latency
				return &credproxy.Injection{Headers: map[string]string{"X": "v"}}, time.Time{}, nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	// singleflight must reduce the actual call count well below goroutine count.
	if n := int(callCount.Load()); n > 5 {
		t.Errorf("fn called %d times for %d concurrent callers; singleflight not working", n, goroutines)
	}
}

func TestTTLCache_do_cachesPersist(t *testing.T) {
	var c ttlCache
	var calls int

	inj := &credproxy.Injection{Headers: map[string]string{"X": "1"}}
	for i := 0; i < 5; i++ {
		got, err := c.do("key", func() (*credproxy.Injection, time.Time, error) {
			calls++
			return inj, time.Now().Add(time.Hour), nil
		})
		if err != nil || got == nil {
			t.Fatalf("call %d: unexpected error or nil inj", i)
		}
	}
	// After the first call caches the result, get() should return it without calling fn again.
	// The first do() always calls fn (no pre-check), but subsequent do() calls see the cache via get().
	// Note: ttlCache.do doesn't check the cache itself — Provider.Get does. This test just verifies
	// that set() is called correctly so subsequent get() calls succeed.
	if _, ok := c.get(time.Now()); !ok {
		t.Error("expected cache entry after do() with non-zero expiry")
	}
}
