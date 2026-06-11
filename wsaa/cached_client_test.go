package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAuthenticator counts Authenticate calls and returns a fixed token.
type fakeAuthenticator struct {
	calls int32
	delay time.Duration
}

func (f *fakeAuthenticator) Authenticate(_ context.Context, _ *x509.Certificate, _ *rsa.PrivateKey, _ string, _ bool) (*TokenAcceso, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return &TokenAcceso{
		Token:      "tok",
		Sign:       "sig",
		Expiration: time.Now().Add(12 * time.Hour),
	}, nil
}

// TestCachedClient_CoalescesConcurrentAuth verifies that N concurrent
// Authenticate calls for the same (cuit, service) result in exactly one
// LoginCms call — the rest must wait and reuse the cached token.
func TestCachedClient_CoalescesConcurrentAuth(t *testing.T) {
	fake := &fakeAuthenticator{delay: 50 * time.Millisecond}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cc.Authenticate(context.Background(), nil, nil, 20111111112, "wsfe", false)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Fatalf("expected exactly 1 LoginCms call, got %d", n)
	}
}

// TestCachedClient_DifferentKeysDontBlock verifies that different
// (cuit, service) pairs authenticate independently.
func TestCachedClient_DifferentKeysDontBlock(t *testing.T) {
	fake := &fakeAuthenticator{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	ctx := context.Background()
	if _, err := cc.Authenticate(ctx, nil, nil, 20111111112, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.Authenticate(ctx, nil, nil, 20222222223, "wsfe", false); err != nil {
		t.Fatal(err)
	}

	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected 2 LoginCms calls (one per CUIT), got %d", n)
	}

	// Second round hits the cache — no new calls.
	if _, err := cc.Authenticate(ctx, nil, nil, 20111111112, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected cache hit, got %d calls", n)
	}
}
