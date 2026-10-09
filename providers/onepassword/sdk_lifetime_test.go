package onepassword

import (
	"context"
	"runtime"
	"testing"
	"time"

	sdk "github.com/1password/onepassword-sdk-go"
)

type blockingSecrets struct {
	sdk.SecretsAPI
	entered chan string
	release chan struct{}
}

func (s *blockingSecrets) Resolve(ctx context.Context, ref string) (string, error) {
	s.entered <- ref
	select {
	case <-s.release:
		return "synthetic-value", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func lifetimeResolver(api *blockingSecrets, released chan struct{}) Resolver {
	client := &sdk.Client{SecretsAPI: api}
	runtime.SetFinalizer(client, func(*sdk.Client) { close(released) })
	return newSDKResolver(client)
}

func TestSDKResolverRetainsClientUntilResolveCompletes(t *testing.T) {
	api := &blockingSecrets{entered: make(chan string, 1), release: make(chan struct{})}
	released := make(chan struct{})
	resolver := lifetimeResolver(api, released)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 3 {
		runtime.GC()
	}
	select {
	case <-released:
		t.Fatal("client released while resolver is reachable")
	default:
	}
	done := make(chan error, 1)
	go func(r Resolver) { _, err := r.Resolve(ctx, "op://test/item/field"); done <- err }(resolver)
	resolver = nil
	select {
	case ref := <-api.entered:
		if ref != "op://test/item/field" {
			t.Fatal("reference not forwarded")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for range 3 {
		runtime.GC()
	}
	select {
	case <-released:
		t.Fatal("client released during secret resolution")
	default:
	}
	close(api.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitClientRelease(t, released)
}

func awaitClientRelease(t *testing.T, released <-chan struct{}) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		runtime.GC()
		select {
		case <-released:
			return
		case <-deadline:
			t.Fatal("unused SDK client retained indefinitely")
		case <-time.After(time.Millisecond):
		}
	}
}
