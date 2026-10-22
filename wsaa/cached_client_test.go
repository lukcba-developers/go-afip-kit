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

// fixtureCert is the certificate used by the cache tests. They use a fake
// authenticator, so only its DER (the fingerprint in the cache key) matters.
var fixtureCert = &x509.Certificate{Raw: []byte("certificado de prueba")}

// testKey returns the "wsfe" cache key of fixtureCert.
func testKey(t *testing.T, cuit int64, production bool) string {
	t.Helper()
	k, err := cacheKey(fixtureCert, cuit, "wsfe", production)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

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
			_, err := cc.Authenticate(context.Background(), fixtureCert, nil, 20111111112, "wsfe", false)
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
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, 20111111112, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, 20222222223, "wsfe", false); err != nil {
		t.Fatal(err)
	}

	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected 2 LoginCms calls (one per CUIT), got %d", n)
	}

	// Second round hits the cache — no new calls.
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, 20111111112, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected cache hit, got %d calls", n)
	}
}

// TestCachedClient_SeparatesEnvironments verifies that homologacion and
// produccion tokens for the same (cuit, service) never share a cache entry.
func TestCachedClient_SeparatesEnvironments(t *testing.T) {
	fake := &fakeAuthenticator{}
	store := NewMemoryStore()
	cc := NewCachedClient(nil, store)
	cc.client = fake

	ctx := context.Background()
	const cuit = 20111111112
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, cuit, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, cuit, "wsfe", true); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected 2 LoginCms calls (one per environment), got %d", n)
	}

	for _, k := range []string{testKey(t, 20111111112, false), testKey(t, 20111111112, true)} {
		if tok, _ := store.Get(ctx, k); tok == nil {
			t.Errorf("expected token cached under %q", k)
		}
	}

	// Invalidating homologacion must not touch produccion.
	if err := cc.Invalidate(ctx, fixtureCert, cuit, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if tok, _ := store.Get(ctx, testKey(t, 20111111112, false)); tok != nil {
		t.Error("homo token should have been invalidated")
	}
	if tok, _ := store.Get(ctx, testKey(t, 20111111112, true)); tok == nil {
		t.Error("prod token should survive invalidating homo")
	}

	// Next homo call re-authenticates; prod still hits the cache.
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, cuit, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, cuit, "wsfe", true); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 3 {
		t.Fatalf("expected 3 LoginCms calls, got %d", n)
	}
}
