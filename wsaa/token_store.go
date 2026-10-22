package wsaa

import (
	"context"
	"sync"
)

// TokenStore is the interface for caching WSAA tokens.
//
// Implementations may use Redis, in-memory maps, files, etc.
// CachedClient uses keys of the form
// "wsaa:{homo|prod}:{cuit}:{service}:{cert-sha256}", so each environment and
// each certificate has its own entry.
type TokenStore interface {
	// Get retrieves a cached token. Returns nil, nil if not found.
	Get(ctx context.Context, key string) (*TokenAcceso, error)

	// Set stores a token with the given key. Implementations should
	// use the token's Expiration to set a TTL.
	Set(ctx context.Context, key string, token *TokenAcceso) error

	// Delete removes a cached token.
	Delete(ctx context.Context, key string) error
}

// MemoryStore is a concurrency-safe in-memory TokenStore.
// Suitable for testing and single-instance deployments.
// For multi-instance deployments, use a Redis-backed implementation.
type MemoryStore struct {
	mu     sync.RWMutex
	tokens map[string]*TokenAcceso
}

// NewMemoryStore creates a new in-memory token store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tokens: make(map[string]*TokenAcceso)}
}

func (m *MemoryStore) Get(_ context.Context, key string) (*TokenAcceso, error) {
	m.mu.RLock()
	t, ok := m.tokens[key]
	m.mu.RUnlock()

	if !ok {
		return nil, nil
	}
	if t.IsExpired(0) {
		m.mu.Lock()
		delete(m.tokens, key)
		m.mu.Unlock()
		return nil, nil
	}
	return t, nil
}

func (m *MemoryStore) Set(_ context.Context, key string, token *TokenAcceso) error {
	m.mu.Lock()
	m.tokens[key] = token
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.tokens, key)
	m.mu.Unlock()
	return nil
}
