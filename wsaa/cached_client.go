package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"log/slog"
	"sync"
	"time"
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
// Concurrent Authenticate calls for the same (cuit, service) are coalesced:
// only one goroutine performs the LoginCms while the rest wait and reuse the
// resulting token. This matters because WSAA rejects a new LoginCms while a
// previous TA is still valid (fault coe.alreadyAuthenticated) — without
// coalescing, concurrent callers on a cache miss would race and all but the
// first would fail.
type CachedClient struct {
	client        authenticator
	store         TokenStore
	renewalMargin time.Duration
	logger        *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // one lock per cache key
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

// NewCachedClient creates a CachedClient that caches tokens in the given store.
func NewCachedClient(client *Client, store TokenStore, opts ...CachedOption) *CachedClient {
	cc := &CachedClient{
		client:        client,
		store:         store,
		renewalMargin: DefaultRenewalMargin,
		locks:         make(map[string]*sync.Mutex),
	}
	for _, o := range opts {
		o(cc)
	}
	return cc
}

// cacheKey builds a deterministic key for the token store.
func cacheKey(cuit int64, service string) string {
	return fmt.Sprintf("wsaa:%d:%s", cuit, service)
}

// keyLock returns the mutex for a cache key, creating it on first use.
// The map only grows by one entry per (cuit, service) pair, so it stays tiny.
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
// one from WSAA and caches it. Concurrent calls for the same (cuit, service)
// share a single LoginCms request.
func (cc *CachedClient) Authenticate(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, cuit int64, service string, production bool) (*TokenAcceso, error) {
	k := cacheKey(cuit, service)

	// Fast path: valid cached token, no lock needed.
	token, err := cc.store.Get(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("token store get: %w", err)
	}
	if token != nil && !token.IsExpired(cc.renewalMargin) {
		return token, nil
	}

	// Slow path: serialize per key so only one goroutine hits WSAA.
	lock := cc.keyLock(k)
	lock.Lock()
	defer lock.Unlock()

	// Re-check the store: another goroutine may have refreshed the token
	// while we waited for the lock.
	token, err = cc.store.Get(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("token store get: %w", err)
	}
	if token != nil && !token.IsExpired(cc.renewalMargin) {
		return token, nil
	}

	token, err = cc.client.Authenticate(ctx, cert, key, service, production)
	if err != nil {
		return nil, err
	}

	// Store in cache (best-effort, don't fail the operation)
	if storeErr := cc.store.Set(ctx, k, token); storeErr != nil && cc.logger != nil {
		cc.logger.Warn("wsaa: token store set failed", "key", k, "error", storeErr)
	}

	return token, nil
}

// Invalidate removes a cached token, forcing re-authentication on next call.
func (cc *CachedClient) Invalidate(ctx context.Context, cuit int64, service string) error {
	return cc.store.Delete(ctx, cacheKey(cuit, service))
}
