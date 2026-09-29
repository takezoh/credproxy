package onepassword

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	mu     sync.Mutex
	values map[string]string
	calls  map[string]int
	fail   string
}

func (f *fakeResolver) Resolve(_ context.Context, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[ref]++
	if ref == f.fail {
		return "", errors.New("upstream detail")
	}
	return f.values[ref], nil
}
func TestPreloadAndRefreshAtomic(t *testing.T) {
	f := &fakeResolver{values: map[string]string{"op://a": "old-a", "op://b": "old-b"}, calls: map[string]int{}}
	p, err := New(f, []Credential{{Name: "a", Ref: "op://a", Preload: true}, {Name: "b", Ref: "op://b", Preload: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Preload(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.values["op://a"] = "new-a"
	f.fail = "op://b"
	f.mu.Unlock()
	if err = p.RefreshMany(context.Background(), nil, true); err == nil || !strings.Contains(err.Error(), "upstream detail") {
		t.Fatalf("refresh error = %v", err)
	}
	for _, name := range []string{"a", "b"} {
		v, err := p.Get(context.Background(), name)
		if err != nil || v != "old-"+name {
			t.Fatalf("%s = %q, %v", name, v, err)
		}
	}
	f.mu.Lock()
	f.fail = ""
	f.mu.Unlock()
	if err = p.RefreshMany(context.Background(), nil, true); err != nil {
		t.Fatal(err)
	}
	v, err := p.Get(context.Background(), "a")
	if err != nil || v != "new-a" {
		t.Fatalf("a = %q, %v", v, err)
	}
}

func TestTTLCache(t *testing.T) {
	f := &fakeResolver{values: map[string]string{"op://a": "first"}, calls: map[string]int{}}
	p, err := New(f, []Credential{{Name: "a", Ref: "op://a", TTL: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		v, err := p.Get(context.Background(), "a")
		if err != nil || v != "first" {
			t.Fatalf("get = %q, %v", v, err)
		}
	}
	if f.calls["op://a"] != 1 {
		t.Fatalf("calls = %d", f.calls["op://a"])
	}
	f.values["op://a"] = "second"
	now = now.Add(time.Minute)
	v, err := p.Get(context.Background(), "a")
	if err != nil || v != "second" || f.calls["op://a"] != 2 {
		t.Fatalf("expired get = %q, %v, calls=%d", v, err, f.calls["op://a"])
	}
}

type orderedResolver struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (r *orderedResolver) Resolve(context.Context, string) (string, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
		<-r.release
		return "old", nil
	}
	return "new", nil
}

func TestRefreshWaitsForInFlightGetAndReplacesIt(t *testing.T) {
	r := &orderedResolver{started: make(chan struct{}), release: make(chan struct{})}
	p, err := New(r, []Credential{{Name: "a", Ref: "op://a", TTL: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	getDone := make(chan error, 1)
	go func() { _, err := p.Get(context.Background(), "a"); getDone <- err }()
	<-r.started
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- p.RefreshMany(context.Background(), []string{"a"}, false) }()
	close(r.release)
	if err := <-getDone; err != nil {
		t.Fatal(err)
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	value, err := p.Get(context.Background(), "a")
	if err != nil || value != "new" || r.calls.Load() != 2 {
		t.Fatalf("cached value=%q calls=%d err=%v", value, r.calls.Load(), err)
	}
}
