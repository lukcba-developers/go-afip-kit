package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
	"github.com/lukcba-developers/go-afip-kit/soap"
)

// seqAuthenticator issues a distinct token per LoginCms ("tok-1", "tok-2",
// ...), or fails with err when set.
type seqAuthenticator struct {
	calls int32
	delay time.Duration
	err   error
}

func (s *seqAuthenticator) Authenticate(context.Context, *x509.Certificate, *rsa.PrivateKey, string, bool) (*TokenAcceso, error) {
	n := atomic.AddInt32(&s.calls, 1)
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &TokenAcceso{Token: fmt.Sprintf("tok-%d", n), Sign: "s", Expiration: time.Now().Add(12 * time.Hour)}, nil
}

var err600 = fmt.Errorf("wsfev1: %w", afipkit.NewAFIPError([]afipkit.AFIPErrorDetail{{Code: 600, Msg: "ValidacionDeToken: No validaron las fechas del token"}}))

func TestCallWithAuthRetry_RenewsOnceOn600(t *testing.T) {
	fake := &seqAuthenticator{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	var used []string
	err := cc.CallWithAuthRetry(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, func(tok *TokenAcceso) error {
		used = append(used, tok.Token)
		if tok.Token == "tok-1" {
			return err600
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(used) != 2 || used[0] != "tok-1" || used[1] != "tok-2" {
		t.Errorf("expected call with tok-1 then tok-2, got %v", used)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Errorf("expected 2 LoginCms (initial + renew), got %d", n)
	}
}

func TestCallWithAuthRetry_RetriesOnlyOnce(t *testing.T) {
	fake := &seqAuthenticator{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	calls := 0
	err := cc.CallWithAuthRetry(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, func(*TokenAcceso) error {
		calls++
		return err600
	})
	if !afipkit.IsTokenRechazado(err) {
		t.Fatalf("expected the 600 to surface, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected exactly 2 calls, got %d", calls)
	}
}

func TestCallWithAuthRetry_OtherErrorsPassThrough(t *testing.T) {
	fake := &seqAuthenticator{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	business := afipkit.NewAFIPError([]afipkit.AFIPErrorDetail{{Code: 10048}})
	calls := 0
	err := cc.CallWithAuthRetry(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, func(*TokenAcceso) error {
		calls++
		return business
	})
	if !errors.Is(err, business) || calls != 1 {
		t.Errorf("expected the business error after 1 call, got %v (%d calls)", err, calls)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Errorf("no renewal expected, got %d LoginCms", n)
	}
}

func TestRenew_UsesTokenRenewedByOthers(t *testing.T) {
	fake := &seqAuthenticator{}
	store := NewMemoryStore()
	cc := NewCachedClient(nil, store)
	cc.client = fake

	stale := &TokenAcceso{Token: "stale", Sign: "s", Expiration: time.Now().Add(time.Hour * 12)}
	_ = store.Set(context.Background(), testKey(t, 20111111112, true),
		&TokenAcceso{Token: "renewed-by-other", Sign: "s", Expiration: time.Now().Add(12 * time.Hour)})

	tok, err := cc.Renew(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, stale)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "renewed-by-other" {
		t.Errorf("expected the newer stored token, got %q", tok.Token)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Errorf("expected no LoginCms, got %d", n)
	}
}

func TestRenew_AlreadyAuthenticatedKeepsStoredToken(t *testing.T) {
	fake := &seqAuthenticator{err: &soap.Fault{Code: "ns1:coe.alreadyAuthenticated", String: "El CEE ya posee un TA valido"}}
	store := NewMemoryStore()
	cc := NewCachedClient(nil, store)
	cc.client = fake

	k := testKey(t, 20111111112, true)
	stale := &TokenAcceso{Token: "stale", Sign: "s", Expiration: time.Now().Add(12 * time.Hour)}
	_ = store.Set(context.Background(), k, stale)

	_, err := cc.Renew(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, stale)
	if !IsAlreadyAuthenticated(err) {
		t.Fatalf("expected alreadyAuthenticated, got %v", err)
	}
	// The stored token must survive: the 600 may have been transient.
	if tok, _ := store.Get(context.Background(), k); tok == nil || tok.Token != "stale" {
		t.Errorf("stored token must not be deleted, got %+v", tok)
	}
}

func TestRenew_ConcurrentReplicasLoginOnce(t *testing.T) {
	fake := &seqAuthenticator{delay: 30 * time.Millisecond}
	store, locker := NewMemoryStore(), NewMemoryLocker()
	k := testKey(t, 20111111112, true)
	stale := &TokenAcceso{Token: "stale", Sign: "s", Expiration: time.Now().Add(12 * time.Hour)}
	_ = store.Set(context.Background(), k, stale)

	replicas := []*CachedClient{newReplica(fake, store, locker), newReplica(fake, store, locker)}
	var wg sync.WaitGroup
	tokens := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(cc *CachedClient) {
			defer wg.Done()
			tok, err := cc.Renew(context.Background(), fixtureCert, nil, 20111111112, "wsfe", true, stale)
			if err != nil {
				t.Errorf("renew: %v", err)
				return
			}
			tokens <- tok.Token
		}(replicas[i%2])
	}
	wg.Wait()
	close(tokens)

	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Fatalf("expected exactly 1 LoginCms for 10 concurrent renewals, got %d", n)
	}
	for tok := range tokens {
		if tok != "tok-1" {
			t.Errorf("every caller should get the renewed token, got %q", tok)
		}
	}
}
