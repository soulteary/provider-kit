package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	// A non-empty token means this caller now owns the key; it identifies THIS
	// claim and must be handed back to Abandon. An empty token means the key
	// was not claimed: existing holds the completed result if one has been
	// recorded, or nil when another caller holds the claim but has not
	// finished yet.
	Reserve(ctx context.Context, key string, ttl time.Duration) (token string, existing *SendResult, err error)

	// Abandon releases a claim that will never produce a result, so a failed
	// attempt does not block retries for the whole TTL.
	//
	// It releases only the claim identified by token. A send that overruns its
	// reservation TTL lets a second caller claim the same key; without the
	// token the first caller's late Abandon would delete that second,
	// still-resultless claim and let a third caller into the provider
	// concurrently with the second -- the very duplicate this type prevents.
	Abandon(ctx context.Context, key, token string) error

	// Finalize records the outcome of the claim identified by token.
	//
	// Like Abandon it is fenced by the token, and for the same reason. Set
	// takes no token, so a send that overran its reservation TTL could
	// complete after a second caller had claimed the same key and replace
	// that caller's entry -- overwriting a completed result, or erasing an
	// in-flight claim -- leaving two callers with different answers for one
	// idempotency key. A write whose claim has been superseded must be
	// dropped, not applied.
	//
	// The result passed in belongs to the store; the caller does not retain
	// it.
	//
	// A nil result records nothing: it RELEASES the claim, so the next caller
	// is let through immediately. An implementation must not store it as an
	// outcome -- Reserve reports a recorded outcome by returning it, and a nil
	// one is indistinguishable from the claim still being in flight.
	Finalize(ctx context.Context, key, token string, result *SendResult, ttl time.Duration) error
}

// ErrClaimSuperseded is returned by Finalize when the claim it was given has
// since been taken over, so the outcome was not recorded.
var ErrClaimSuperseded = errors.New("idempotency claim superseded")

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
	//
	// The two must stay distinguishable, so a nil outcome is never RECORDED:
	// Finalize releases the claim instead. A Provider is free to return
	// (nil, nil), and storing that made the completed entry read as an
	// unfinished claim.
	result    *SendResult
	expiresAt time.Time
	// token identifies the caller holding an as-yet-resultless claim, so a
	// late Abandon from a previous holder cannot release a newer one.
	token string
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
func (s *MemoryIdempotencyStore) Reserve(_ context.Context, key string, ttl time.Duration) (string, *SendResult, error) {
	token, err := newClaimToken()
	if err != nil {
		return "", nil, err
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[key]; ok && now.Before(entry.expiresAt) {
		if entry.result == nil {
			return "", nil, nil // claimed by someone else, still in flight
		}
		// Copy: the stored result is shared by every caller that hits this key.
		return "", cloneSendResult(entry.result), nil
	}

	// Claim the key with no result yet; Set records the outcome later.
	s.entries[key] = &idempotencyEntry{expiresAt: now.Add(ttl), token: token}
	return token, nil, nil
}

// Abandon implements Reserver.
func (s *MemoryIdempotencyStore) Abandon(_ context.Context, key, token string) error {
	if token == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Only drop a claim that has not recorded a result AND still belongs to
	// the caller abandoning it. A send that overran its TTL must not release
	// the claim a later caller has since taken.
	if entry, ok := s.entries[key]; ok && entry.result == nil && entry.token == token {
		delete(s.entries, key)
	}
	return nil
}

// Finalize implements Reserver.
func (s *MemoryIdempotencyStore) Finalize(_ context.Context, key, token string, result *SendResult, ttl time.Duration) error {
	if token == "" {
		// An empty token owns nothing. A completed entry carries no token, so
		// accepting one here would let a tokenless call overwrite or delete an
		// outcome some other caller recorded.
		return ErrClaimSuperseded
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[key]; ok && entry.token != token {
		// Someone else holds this key now, or has already recorded an outcome
		// for it. Theirs is the current answer.
		return ErrClaimSuperseded
	}

	if result == nil {
		// Nothing to record, so RELEASE the claim rather than store a nil.
		// A stored nil is indistinguishable from an unfinished claim, and it
		// read as one: every retry got ErrSendInFlight until the TTL elapsed,
		// and then sent anyway. Releasing lets the next caller through
		// immediately, which is what a store with no recorded outcome means.
		delete(s.entries, key)
		return nil
	}

	// No entry at all means the claim expired and nothing replaced it. A send
	// did go out, so recording its outcome is still the best answer available.
	s.entries[key] = &idempotencyEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// cloneSendResult returns an independent copy of a stored result.
//
// Copying the struct alone shares the Metadata MAP and the Error pointer with
// every other caller that hits this key, so one caller adding metadata -- the
// result constructors allocate that map, and WithMetadata writes into it --
// mutated what the others saw, and raced with them while doing it.
func cloneSendResult(src *SendResult) *SendResult {
	if src == nil {
		return nil
	}

	out := *src
	if src.Metadata != nil {
		out.Metadata = make(map[string]string, len(src.Metadata))
		for k, v := range src.Metadata {
			out.Metadata[k] = v
		}
	}
	if src.Error != nil {
		errCopy := *src.Error
		out.Error = &errCopy
	}
	return &out
}

// newClaimToken returns an unguessable identifier for one reservation.
func newClaimToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("provider: generating an idempotency claim token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
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
	return cloneSendResult(entry.result), true, nil
}

// Set stores a result with the given key and TTL
func (s *MemoryIdempotencyStore) Set(ctx context.Context, key string, result *SendResult, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if result == nil {
		// The same invariant Finalize keeps: a nil outcome is never RECORDED.
		// Storing it left an entry that Get read as a miss but Reserve read as
		// an open claim -- with no token, so nothing could ever release it --
		// and every send for the key was answered ErrSendInFlight until the
		// TTL elapsed.
		//
		// Only a COMPLETED entry is cleared. An entry with no result is an
		// active claim held by a sender that is still running, and Set carries
		// no token, so deleting it would release a claim it does not own: the
		// next Reserve would hand out a second token and the message would go
		// out twice. That is the duplicate this whole type exists to prevent,
		// and it is the worse failure of the two.
		if entry, ok := s.entries[key]; ok && entry.result != nil {
			delete(s.entries, key)
		}
		return nil
	}

	// No token: an entry carrying a result is no longer an open claim, and
	// Abandon must not remove it.
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

	token, existing, err := reserver.Reserve(ctx, cacheKey, p.ttl)
	if err != nil {
		return nil, err
	}
	if token == "" {
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
			//
			// On a cleanup context for the same reason Abandon is: an outcome
			// that arrives after the deadline, or a cancellation landing
			// between the provider returning and this call, would otherwise
			// be rejected by a context-aware store -- so a message that DID
			// go out is not recorded, and a retry sends it again once the
			// reservation expires.
			finalizeCtx, cancel := cleanupContext(ctx)
			_ = reserver.Finalize(finalizeCtx, cacheKey, token, cloneSendResult(result), p.ttl)
			cancel()
		} else {
			// No outcome to record. Release the claim so a retry is not
			// blocked for the whole TTL.
			//
			// On a cleanup context, because the usual reason there is no
			// result is that ctx was cancelled or timed out -- and a
			// context-aware store would reject the cleanup on that very ctx,
			// leaving the reservation standing and every retry answered with
			// ErrSendInFlight until the TTL elapsed.
			cleanupCtx, cancel := cleanupContext(ctx)
			_ = reserver.Abandon(cleanupCtx, cacheKey, token)
			cancel()
		}
		return result, err
	}

	// Record the outcome. A failure here is deliberately not surfaced: the
	// message has already gone out, and returning an error would push a
	// caller that retries on error into sending it a second time. The cost is
	// that a later retry is not deduplicated, which is strictly better than
	// duplicating a send that already succeeded.
	//
	// A CLONE is handed over, and the caller keeps the original. Passing the
	// provider's own pointer let the caller go on mutating what the store had
	// cached -- WithMetadata writes into the Metadata map the result
	// constructors allocate -- and raced with readers cloning that same map.
	//
	// On a cleanup context: the message has already gone out, so recording it
	// must not be skipped just because the caller's deadline has since passed.
	//
	// A nil result here -- which the Provider interface permits -- records
	// nothing and releases the claim, because there is no outcome to hand a
	// retry. The retry then sends again, exactly as it does without a
	// Reserver, instead of being answered ErrSendInFlight for the whole TTL.
	finalizeCtx, cancel := cleanupContext(ctx)
	_ = reserver.Finalize(finalizeCtx, cacheKey, token, cloneSendResult(result), p.ttl)
	cancel()

	return result, nil
}

// cleanupContext derives a bounded context for finishing a reservation.
//
// Detached from ctx's cancellation: by the time a send has an outcome to
// record -- or a claim to release -- the caller's context may already be
// cancelled or past its deadline, and a context-aware store would reject the
// write on it. The errors are discarded, so that failure is silent: an
// outcome is lost and its reservation stands until expiry, after which a
// retry sends the message a second time.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}

// cleanupTimeout bounds finishing a reservation once the send is over.
const cleanupTimeout = 5 * time.Second

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
			_ = p.store.Set(ctx, cacheKey, cloneSendResult(result), p.ttl)
		}
		return result, err
	}

	// See Send: a failure to record an outcome that already happened must not
	// be reported as a send failure.
	// A clone, as in sendWithReservation: the caller keeps the original and
	// must not be able to mutate what the store cached.
	_ = p.store.Set(ctx, cacheKey, cloneSendResult(result), p.ttl)
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
