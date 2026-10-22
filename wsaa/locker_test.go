package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// newReplica simulates one process of a multi-replica deployment: its own
// CachedClient (own in-process locks) over a shared store and locker.
func newReplica(fake authenticator, store TokenStore, locker Locker, opts ...CachedOption) *CachedClient {
	cc := NewCachedClient(nil, store, append([]CachedOption{WithLocker(locker)}, opts...)...)
	cc.client = fake
	cc.lockPoll = 5 * time.Millisecond
	return cc
}

// TestLocker_CoalescesAcrossReplicas verifies that several CachedClients
// (separate processes) sharing a store and a Locker make exactly one LoginCms.
func TestLocker_CoalescesAcrossReplicas(t *testing.T) {
	fake := &fakeAuthenticator{delay: 50 * time.Millisecond}
	store, locker := NewMemoryStore(), NewMemoryLocker()
	replicas := []*CachedClient{
		newReplica(fake, store, locker),
		newReplica(fake, store, locker),
		newReplica(fake, store, locker),
	}

	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(cc *CachedClient) {
			defer wg.Done()
			if _, err := cc.Authenticate(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true); err != nil {
				errs <- err
			}
		}(replicas[i%len(replicas)])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Fatalf("expected exactly 1 LoginCms across replicas, got %d", n)
	}

	// The lock was released.
	if ok, _ := locker.TryLock(context.Background(), testKey(t, 20111111112, true)+":lock", "someone", time.Second); !ok {
		t.Error("lock should be released after login")
	}
}

// TestLocker_WaitsUntilContextDone verifies a replica does not log in while
// another owner holds the lock, and gives up when its context ends.
func TestLocker_WaitsUntilContextDone(t *testing.T) {
	fake := &fakeAuthenticator{}
	store, locker := NewMemoryStore(), NewMemoryLocker()
	_, _ = locker.TryLock(context.Background(), testKey(t, 20111111112, false)+":lock", "other-pod", time.Minute)

	cc := newReplica(fake, store, locker)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := cc.Authenticate(ctx, fixtureCert, nil, 20111111112, "wsfe", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Fatalf("must not call LoginCms while another owner holds the lock, got %d calls", n)
	}
}

// TestLocker_ReusesTokenStoredByLockHolder verifies a waiting replica returns
// the token the lock holder stored, without logging in itself.
func TestLocker_ReusesTokenStoredByLockHolder(t *testing.T) {
	fake := &fakeAuthenticator{}
	store, locker := NewMemoryStore(), NewMemoryLocker()
	lockKey := testKey(t, 20111111112, false) + ":lock"
	_, _ = locker.TryLock(context.Background(), lockKey, "other-pod", time.Minute)

	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = store.Set(context.Background(), testKey(t, 20111111112, false),
			&TokenAcceso{Token: "from-other-pod", Sign: "s", Expiration: time.Now().Add(12 * time.Hour)})
	}()

	cc := newReplica(fake, store, locker)
	tok, err := cc.Authenticate(context.Background(), fixtureCert, nil, 20111111112, "wsfe", false)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "from-other-pod" {
		t.Errorf("expected the other pod's token, got %q", tok.Token)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Fatalf("expected no LoginCms, got %d", n)
	}
}

// TestLocker_TakesOverExpiredLock verifies that if the holder dies, the lock
// expires after its TTL and another replica logs in.
func TestLocker_TakesOverExpiredLock(t *testing.T) {
	fake := &fakeAuthenticator{}
	store, locker := NewMemoryStore(), NewMemoryLocker()
	_, _ = locker.TryLock(context.Background(), testKey(t, 20111111112, false)+":lock", "dead-pod", 30*time.Millisecond)

	cc := newReplica(fake, store, locker)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cc.Authenticate(ctx, fixtureCert, nil, 20111111112, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Fatalf("expected 1 LoginCms after the lock expired, got %d", n)
	}
}

type failingLocker struct{}

func (failingLocker) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("redis down")
}
func (failingLocker) Unlock(context.Context, string, string) error { return nil }

func TestLocker_ErrorIsReturned(t *testing.T) {
	fake := &fakeAuthenticator{}
	cc := newReplica(fake, NewMemoryStore(), failingLocker{})
	if _, err := cc.Authenticate(context.Background(), fixtureCert, nil, 20111111112, "wsfe", false); err == nil {
		t.Fatal("expected lock error")
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Fatalf("must not log in without the lock, got %d calls", n)
	}
}

// alreadyAuthFake simulates a replica without the lock (e.g. an older deploy)
// that logs in first: WSAA answers coe.alreadyAuthenticated and that replica's
// token is already in the store.
type alreadyAuthFake struct {
	store TokenStore
	key   string
}

func (f *alreadyAuthFake) Authenticate(ctx context.Context, _ *x509.Certificate, _ *rsa.PrivateKey, _ string, _ bool) (*TokenAcceso, error) {
	_ = f.store.Set(ctx, f.key, &TokenAcceso{Token: "stored-by-other", Sign: "s", Expiration: time.Now().Add(12 * time.Hour)})
	return nil, &soap.Fault{Code: "ns1:coe.alreadyAuthenticated", String: "El CEE ya posee un TA valido para el acceso al WSN solicitado"}
}

func TestCachedClient_AlreadyAuthenticatedReusesStoredToken(t *testing.T) {
	store := NewMemoryStore()
	cc := NewCachedClient(nil, store)
	cc.client = &alreadyAuthFake{store: store, key: testKey(t, 20111111112, true)}

	tok, err := cc.Authenticate(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatalf("expected the stored token, got error %v", err)
	}
	if tok.Token != "stored-by-other" {
		t.Errorf("got %q", tok.Token)
	}
}

func TestMemoryLocker_OwnerSemantics(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryLocker()
	if ok, _ := l.TryLock(ctx, "k", "a", time.Minute); !ok {
		t.Fatal("a should acquire")
	}
	if ok, _ := l.TryLock(ctx, "k", "b", time.Minute); ok {
		t.Fatal("b must not acquire while a holds it")
	}
	_ = l.Unlock(ctx, "k", "b") // not the owner: no effect
	if ok, _ := l.TryLock(ctx, "k", "b", time.Minute); ok {
		t.Fatal("unlock by a non-owner must not release the lock")
	}
	_ = l.Unlock(ctx, "k", "a")
	if ok, _ := l.TryLock(ctx, "k", "b", time.Minute); !ok {
		t.Fatal("b should acquire after a released")
	}
}
