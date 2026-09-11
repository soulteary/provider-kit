package provider

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrSendInFlight reports that another goroutine or process is already sending
// for this idempotency key and has not finished yet.
//
// Returning it is what makes the guarantee real: the alternative is to send a
// second time and call that idempotent.
var ErrSendInFlight = errors.New("a send for this idempotency key is already in flight")

// Reserver is an IdempotencyStore that can claim a key atomically.
//
// Without this, an idempotent send is a check-then-act: two concurrent
// requests carrying the same key both miss the store, both call the provider,
// and two messages go out -- which is precisely the case idempotency exists
// for. Reserve makes the claim and the lookup one operation.
//
// Implementations must be atomic across every process sharing the store. For
// Redis that is SET key value NX PX ttl.
type Reserver interface {
	// Reserve atomically claims key for ttl.
	//
	// It returns claimed=true when this caller now owns the key. When it
	// returns claimed=false, existing holds the completed result if one has
	// been recorded, or nil when another caller holds the claim but has not
	// finished yet.
	Reserve(ctx context.Context, key string, ttl time.Duration) (claimed bool, existing *SendResult, err error)

	// Abandon releases a claim that will never produce a result, so a failed
	// attempt does not block retries for the whole TTL.
	Abandon(ctx context.Context, key string) error
}

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
	mu        sync.RWMutex
	entries   map[string]*idempotencyEntry
	done      chan struct{}
	closeOnce sync.Once
}

type idempotencyEntry struct {
	// result is nil while a send is in flight and set once it completes.
	result    *SendResult
	expiresAt time.Time
}

// NewMemoryIdempotencyStore creates a new in-memory idempotency store.
//
// The store owns a background cleanup goroutine; call Close when done with it.
// Note that an in-memory store only deduplicates within one process: a
// multi-instance deployment needs a shared store, or the same key sent to two
// instances produces two messages.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	store := &MemoryIdempotencyStore{
		entries: make(map[string]*idempotencyEntry),
		done:    make(chan struct{}),
	}
	// Start cleanup goroutine
	go store.cleanup()
	return store
}

// Close stops the cleanup goroutine.
//
// Without it the goroutine ran forever and kept the store reachable, so every
// store ever created leaked both a goroutine and its entries.
func (s *MemoryIdempotencyStore) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

// Reserve implements Reserver.
func (s *MemoryIdempotencyStore) Reserve(_ context.Context, key string, ttl time.Duration) (bool, *SendResult, error) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[key]; ok && now.Before(entry.expiresAt) {
		if entry.result == nil {
			return false, nil, nil // claimed by someone else, still in flight
		}
		// Copy: the stored result is shared by every caller that hits this key.
		result := *entry.result
		return false, &result, nil
	}

	// Claim the key with no result yet; Set records the outcome later.
	s.entries[key] = &idempotencyEntry{expiresAt: now.Add(ttl)}
	return true, nil, nil
}

// Abandon implements Reserver.
func (s *MemoryIdempotencyStore) Abandon(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only drop a claim that has not recorded a result.
	if entry, ok := s.entries[key]; ok && entry.result == nil {
		delete(s.entries, key)
	}
	return nil
}

// Get retrieves a cached result for the given key
func (s *MemoryIdempotencyStore) Get(ctx context.Context, key string) (*SendResult, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}

	if time.Now().After(entry.expiresAt) || entry.result == nil {
		return nil, false, nil
	}

	// Hand back a copy: the stored result is shared by every caller that hits
	// this key, so returning the pointer let one caller mutate what the others
	// see.
	result := *entry.result
	return &result, true, nil
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

	for {
		select {
		case <-ticker.C:
			s.removeExpired()
		case <-s.done:
			return
		}
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

// Send sends a message with idempotency support.
//
// When the store implements Reserver, the key is claimed atomically before the
// provider is called, so two concurrent requests carrying the same key result
// in one send. Without a Reserver the wrapper can only do a check-then-act,
// which does not hold under concurrency; that path is kept for compatibility
// with third-party stores.
//
// A store error is reported rather than treated as a miss. Swallowing it meant
// that when the store was unavailable, idempotency silently switched off and
// the message went out again.
func (p *IdempotentProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	// If no idempotency key, just send directly
	if msg.IdempotencyKey == "" {
		return p.provider.Send(ctx, msg)
	}

	cacheKey := p.buildCacheKey(msg.IdempotencyKey)

	reserver, canReserve := p.store.(Reserver)
	if !canReserve {
		return p.sendUnreserved(ctx, cacheKey, msg)
	}

	claimed, existing, err := reserver.Reserve(ctx, cacheKey, p.ttl)
	if err != nil {
		return nil, err
	}
	if !claimed {
		if existing != nil {
			return existing, nil
		}
		// Somebody else holds the claim and has not finished. Sending now is
		// exactly the duplicate this wrapper exists to prevent.
		return nil, ErrSendInFlight
	}

	result, err := p.provider.Send(ctx, msg)
	if err != nil {
		if result != nil {
			// A definite outcome: record it so retries see the same answer.
			_ = p.store.Set(ctx, cacheKey, result, p.ttl)
		} else {
			// No outcome to record. Release the claim so a retry is not
			// blocked for the whole TTL.
			_ = reserver.Abandon(ctx, cacheKey)
		}
		return result, err
	}

	// Record the outcome. A failure here is deliberately not surfaced: the
	// message has already gone out, and returning an error would push a
	// caller that retries on error into sending it a second time. The cost is
	// that a later retry is not deduplicated, which is strictly better than
	// duplicating a send that already succeeded.
	_ = p.store.Set(ctx, cacheKey, result, p.ttl)

	return result, nil
}

// sendUnreserved is the legacy check-then-act path, used when the configured
// store does not implement Reserver.
func (p *IdempotentProvider) sendUnreserved(ctx context.Context, cacheKey string, msg *Message) (*SendResult, error) {
	result, found, err := p.store.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}
	if found {
		return result, nil
	}

	result, err = p.provider.Send(ctx, msg)
	if err != nil {
		if result != nil {
			_ = p.store.Set(ctx, cacheKey, result, p.ttl)
		}
		return result, err
	}

	// See Send: a failure to record an outcome that already happened must not
	// be reported as a send failure.
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
