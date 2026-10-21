package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"log/slog"
	"sync"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

// DefaultRenewalMargin is how early to renew a token before it expires.
const DefaultRenewalMargin = 30 * time.Minute

// authenticator is the minimal surface of Client that CachedClient needs.
// It exists so tests can substitute a fake without hitting WSAA.
type authenticator interface {
	Authenticate(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, service string, production bool) (*TokenAcceso, error)
}

// CachedClient wraps a WSAA Client with a TokenStore for automatic caching.
// It avoids redundant LoginCms calls by reusing valid tokens.
//
// Tokens are cached per (environment, cuit, service): homologacion and
// produccion use different WSAA servers and their tokens are not
// interchangeable, so they never share a cache entry.
//
// Concurrent Authenticate calls for the same (environment, cuit, service) are coalesced:
// only one goroutine performs the LoginCms while the rest wait and reuse the
// resulting token. This matters because WSAA rejects a new LoginCms while a
// previous TA is still valid (fault coe.alreadyAuthenticated) — without
// coalescing, concurrent callers on a cache miss would race and all but the
// first would fail.
//
// That coalescing is per process. With several replicas sharing the
// TokenStore, configure WithLocker so only one replica calls LoginCms.
type CachedClient struct {
	client        authenticator
	store         TokenStore
	renewalMargin time.Duration
	logger        *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // one lock per cache key

	locker   Locker // optional, coalesces logins across processes
	lockTTL  time.Duration
	lockPoll time.Duration
}

// CachedOption configures the CachedClient.
type CachedOption func(*CachedClient)

// WithRenewalMargin sets how early to renew tokens before expiration.
func WithRenewalMargin(d time.Duration) CachedOption {
	return func(cc *CachedClient) {
		cc.renewalMargin = d
	}
}

// WithCacheLogger sets a logger for non-fatal cache events (e.g. a TokenStore
// write that fails). Without it, those events are silently ignored.
func WithCacheLogger(l *slog.Logger) CachedOption {
	return func(cc *CachedClient) {
		cc.logger = l
	}
}

// WithLocker enables a distributed lock so that only one process at a time
// calls LoginCms for a given (environment, cuit, service). The others wait,
// polling the TokenStore, and reuse the token the winner stored. Use it when
// several replicas share the TokenStore.
func WithLocker(l Locker) CachedOption {
	return func(cc *CachedClient) {
		cc.locker = l
	}
}

// WithLockTTL sets how long the distributed lock is held at most (default
// DefaultLockTTL). It must exceed the LoginCms duration.
func WithLockTTL(d time.Duration) CachedOption {
	return func(cc *CachedClient) {
		cc.lockTTL = d
	}
}

// NewCachedClient creates a CachedClient that caches tokens in the given store.
func NewCachedClient(client *Client, store TokenStore, opts ...CachedOption) *CachedClient {
	cc := &CachedClient{
		client:        client,
		store:         store,
		renewalMargin: DefaultRenewalMargin,
		locks:         make(map[string]*sync.Mutex),
		lockTTL:       DefaultLockTTL,
		lockPoll:      defaultLockPoll,
	}
	for _, o := range opts {
		o(cc)
	}
	return cc
}

// cacheKey builds a deterministic key for the token store:
// "wsaa:{homo|prod}:{cuit}:{service}".
func cacheKey(cuit int64, service string, production bool) string {
	env := "homo"
	if production {
		env = "prod"
	}
	return fmt.Sprintf("wsaa:%s:%d:%s", env, cuit, service)
}

// keyLock returns the mutex for a cache key, creating it on first use.
// The map only grows by one entry per (environment, cuit, service), so it stays tiny.
func (cc *CachedClient) keyLock(k string) *sync.Mutex {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	l, ok := cc.locks[k]
	if !ok {
		l = &sync.Mutex{}
		cc.locks[k] = l
	}
	return l
}

// Authenticate returns a cached token if still valid, otherwise obtains a new
// one from WSAA and caches it. Concurrent calls for the same
// (environment, cuit, service) share a single LoginCms request: within the
// process always, and across processes when WithLocker is configured.
func (cc *CachedClient) Authenticate(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, cuit int64, service string, production bool) (*TokenAcceso, error) {
	k := cacheKey(cuit, service, production)

	// Fast path: valid cached token, no lock needed.
	if token, err := cc.cached(ctx, k); err != nil || token != nil {
		return token, err
	}
	return cc.obtain(ctx, cert, key, k, service, production, nil)
}

// Renew obtains a new token after a business service rejected `rejected`
// (AFIP error 600, see afipkit.IsTokenRechazado). It never deletes the stored
// token:
//   - if the store already holds a different valid token (another goroutine or
//     replica renewed first), that one is returned;
//   - otherwise it calls LoginCms and, only on success, replaces the stored
//     token.
//
// If WSAA still considers the rejected TA valid it answers
// coe.alreadyAuthenticated: the error is returned and the stored token is left
// in place, so a transient 600 does not leave the CUIT without a token. With
// several replicas, configure WithLocker so the check and the renewal are
// atomic across them.
func (cc *CachedClient) Renew(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, cuit int64, service string, production bool, rejected *TokenAcceso) (*TokenAcceso, error) {
	return cc.obtain(ctx, cert, key, cacheKey(cuit, service, production), service, production, rejected)
}

// CallWithAuthRetry runs call with a valid token. If call fails with AFIP
// error 600 (token rejected), it gets a fresh token with Renew and runs call
// exactly once more. Any other result of call is returned as is.
//
//	err := cc.CallWithAuthRetry(ctx, cert, key, cuit, "wsfe", prod, func(tok *wsaa.TokenAcceso) error {
//	    var err error
//	    res, err = wsfe.SolicitarCAEConReproceso(ctx, tok, cuit, sol)
//	    return err
//	})
//
// A 600 on FECAESolicitar comes in the header Errors: the comprobante was not
// processed, so the retry is safe.
func (cc *CachedClient) CallWithAuthRetry(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, cuit int64, service string, production bool, call func(*TokenAcceso) error) error {
	token, err := cc.Authenticate(ctx, cert, key, cuit, service, production)
	if err != nil {
		return err
	}
	err = call(token)
	if !afipkit.IsTokenRechazado(err) {
		return err
	}
	fresh, rErr := cc.Renew(ctx, cert, key, cuit, service, production, token)
	if rErr != nil {
		return fmt.Errorf("%w (renovar token: %w)", err, rErr)
	}
	return call(fresh)
}

// obtain returns a usable token for key k, calling LoginCms only when needed.
// A stored token is usable when it is valid and is not `rejected`.
func (cc *CachedClient) obtain(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, k, service string, production bool, rejected *TokenAcceso) (*TokenAcceso, error) {
	usable := func() (*TokenAcceso, error) {
		token, err := cc.cached(ctx, k)
		if err != nil || token == nil {
			return nil, err
		}
		if rejected != nil && token.Token == rejected.Token {
			return nil, nil
		}
		return token, nil
	}

	// Serialize per key so only one goroutine hits WSAA.
	lock := cc.keyLock(k)
	lock.Lock()
	defer lock.Unlock()

	// Another goroutine may have stored a token while we waited for the lock.
	if token, err := usable(); err != nil || token != nil {
		return token, err
	}

	if cc.locker != nil {
		release, token, err := cc.lockDistributed(ctx, k, usable)
		if err != nil || token != nil {
			return token, err
		}
		defer release()

		// The previous holder may have stored a token right before releasing.
		if token, err := usable(); err != nil || token != nil {
			return token, err
		}
	}
	return cc.login(ctx, cert, key, k, service, production, usable)
}

// cached returns the stored token if it is still valid, or nil.
func (cc *CachedClient) cached(ctx context.Context, k string) (*TokenAcceso, error) {
	token, err := cc.store.Get(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("token store get: %w", err)
	}
	if token != nil && !token.IsExpired(cc.renewalMargin) {
		return token, nil
	}
	return nil, nil
}

// lockDistributed takes the distributed lock for k. While another process
// holds it, it polls usable(); if that yields a token (the holder stored it),
// the token is returned and no lock is taken.
func (cc *CachedClient) lockDistributed(ctx context.Context, k string, usable func() (*TokenAcceso, error)) (release func(), token *TokenAcceso, err error) {
	lockKey := k + ":lock"
	owner, err := newLockOwner()
	if err != nil {
		return nil, nil, fmt.Errorf("wsaa: lock owner: %w", err)
	}

	for {
		acquired, err := cc.locker.TryLock(ctx, lockKey, owner, cc.lockTTL)
		if err != nil {
			return nil, nil, fmt.Errorf("wsaa: distributed lock %s: %w", lockKey, err)
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("wsaa: waiting for distributed lock %s: %w", lockKey, ctx.Err())
		case <-time.After(cc.lockPoll):
		}
		if token, err := usable(); err != nil || token != nil {
			return nil, token, err
		}
	}

	return func() {
		// Release even if ctx was canceled; the TTL covers a failed release.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := cc.locker.Unlock(uctx, lockKey, owner); err != nil && cc.logger != nil {
			cc.logger.Warn("wsaa: distributed unlock failed", "key", lockKey, "error", err)
		}
	}, nil, nil
}

// login calls LoginCms and stores the token.
func (cc *CachedClient) login(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, k, service string, production bool, usable func() (*TokenAcceso, error)) (*TokenAcceso, error) {
	token, err := cc.client.Authenticate(ctx, cert, key, service, production)
	if err != nil {
		// Another process (without the lock, e.g. an older deploy) may have
		// just logged in and stored its token: reuse it if so.
		if IsAlreadyAuthenticated(err) {
			if stored, sErr := usable(); sErr == nil && stored != nil {
				return stored, nil
			}
		}
		return nil, err
	}

	// Store in cache (best-effort, don't fail the operation)
	if storeErr := cc.store.Set(ctx, k, token); storeErr != nil && cc.logger != nil {
		cc.logger.Warn("wsaa: token store set failed", "key", k, "error", storeErr)
	}

	return token, nil
}

// Invalidate removes the cached token for the given environment, forcing
// re-authentication on the next call. The token of the other environment is
// left untouched.
func (cc *CachedClient) Invalidate(ctx context.Context, cuit int64, service string, production bool) error {
	return cc.store.Delete(ctx, cacheKey(cuit, service, production))
}
