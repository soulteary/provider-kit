package provider

import (
	"context"
	"sync"
	"time"
)

// IdempotencyStore defines the interface for storing idempotency records
type IdempotencyStore interface {
	// Get retrieves a cached result for the given key
	Get(ctx context.Context, key string) (*SendResult, bool, error)
	// Set stores a result with the given key and TTL
	Set(ctx context.Context, key string, result *SendResult, ttl time.Duration) error
	// Delete removes a cached result
	Delete(ctx context.Context, key string) error
}

// MemoryIdempotencyStore is an in-memory idempotency store
type MemoryIdempotencyStore struct {
	mu      sync.RWMutex
	entries map[string]*idempotencyEntry
}

type idempotencyEntry struct {
	result    *SendResult
	expiresAt time.Time
}

// NewMemoryIdempotencyStore creates a new in-memory idempotency store
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	store := &MemoryIdempotencyStore{
		entries: make(map[string]*idempotencyEntry),
	}
	// Start cleanup goroutine
	go store.cleanup()
	return store
}

// Get retrieves a cached result for the given key
func (s *MemoryIdempotencyStore) Get(ctx context.Context, key string) (*SendResult, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false, nil
	}

	return entry.result, true, nil
}

// Set stores a result with the given key and TTL
func (s *MemoryIdempotencyStore) Set(ctx context.Context, key string, result *SendResult, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = &idempotencyEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// Delete removes a cached result
func (s *MemoryIdempotencyStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

// cleanup periodically removes expired entries
func (s *MemoryIdempotencyStore) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.removeExpired()
	}
}

// removeExpired removes all expired entries
func (s *MemoryIdempotencyStore) removeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
}

// IdempotentProvider wraps a provider with idempotency support
type IdempotentProvider struct {
	provider Provider
	store    IdempotencyStore
	ttl      time.Duration
}

// IdempotencyConfig contains idempotency configuration
type IdempotencyConfig struct {
	// Store is the idempotency store (default: in-memory)
	Store IdempotencyStore
	// TTL is the cache TTL for idempotency records
	TTL time.Duration
}

// DefaultIdempotencyConfig returns default idempotency configuration
func DefaultIdempotencyConfig() *IdempotencyConfig {
	return &IdempotencyConfig{
		Store: NewMemoryIdempotencyStore(),
		TTL:   5 * time.Minute,
	}
}

// NewIdempotentProvider wraps a provider with idempotency support
func NewIdempotentProvider(provider Provider, config *IdempotencyConfig) *IdempotentProvider {
	if config == nil {
		config = DefaultIdempotencyConfig()
	}
	if config.Store == nil {
		config.Store = NewMemoryIdempotencyStore()
	}
	if config.TTL == 0 {
		config.TTL = 5 * time.Minute
	}

	return &IdempotentProvider{
		provider: provider,
		store:    config.Store,
		ttl:      config.TTL,
	}
}

// Send sends a message with idempotency support
func (p *IdempotentProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	// If no idempotency key, just send directly
	if msg.IdempotencyKey == "" {
		return p.provider.Send(ctx, msg)
	}

	// Check for existing result
	cacheKey := p.buildCacheKey(msg.IdempotencyKey)
	if result, found, err := p.store.Get(ctx, cacheKey); err == nil && found {
		// Return cached result
		return result, nil
	}

	// Send the message
	result, err := p.provider.Send(ctx, msg)
	if err != nil {
		// Cache failure results too (to prevent retry storms)
		if result != nil {
			_ = p.store.Set(ctx, cacheKey, result, p.ttl)
		}
		return result, err
	}

	// Cache successful result (ignore error - don't fail the send if cache fails)
	_ = p.store.Set(ctx, cacheKey, result, p.ttl)

	return result, nil
}

// Channel returns the channel type
func (p *IdempotentProvider) Channel() Channel {
	return p.provider.Channel()
}

// Name returns the provider name
func (p *IdempotentProvider) Name() string {
	return p.provider.Name()
}

// Validate checks if the provider is properly configured
func (p *IdempotentProvider) Validate() error {
	return p.provider.Validate()
}

// buildCacheKey builds a cache key from the idempotency key
func (p *IdempotentProvider) buildCacheKey(idempotencyKey string) string {
	return "idem:" + string(p.provider.Channel()) + ":" + idempotencyKey
}

// WrapWithIdempotency wraps a provider with idempotency support
func WrapWithIdempotency(provider Provider, store IdempotencyStore, ttl time.Duration) Provider {
	return NewIdempotentProvider(provider, &IdempotencyConfig{
		Store: store,
		TTL:   ttl,
	})
}
