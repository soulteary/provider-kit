package provider

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingProvider records how many times Send actually reached the provider.
type countingProvider struct {
	sends  int32
	delay  time.Duration
	inside sync.WaitGroup
}

func (p *countingProvider) Channel() Channel { return ChannelEmail }
func (p *countingProvider) Name() string     { return "counting" }
func (p *countingProvider) Validate() error  { return nil }

func (p *countingProvider) Send(_ context.Context, _ *Message) (*SendResult, error) {
	atomic.AddInt32(&p.sends, 1)
	time.Sleep(p.delay)
	return &SendResult{OK: true, Provider: "counting"}, nil
}

// TestConcurrentSendsWithSameKeySendOnce is the regression test for the
// check-then-act window: two requests carrying the same idempotency key both
// missed the store and both called the provider, so two messages went out --
// the exact case idempotency exists to prevent.
func TestConcurrentSendsWithSameKeySendOnce(t *testing.T) {
	base := &countingProvider{delay: 30 * time.Millisecond}
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	p := NewIdempotentProvider(base, &IdempotencyConfig{Store: store, TTL: time.Minute})

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := NewMessage("test@example.com").WithBody("x").WithIdempotencyKey("same-key")
			_, errs[i] = p.Send(context.Background(), msg)
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&base.sends); got != 1 {
		t.Fatalf("provider was called %d times for one idempotency key, want 1", got)
	}

	// Losers either see the recorded result or are told a send is in flight;
	// neither may silently send again.
	var inFlight, ok int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSendInFlight):
			inFlight++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok+inFlight != callers {
		t.Errorf("accounted for %d of %d callers", ok+inFlight, callers)
	}
}

// TestSendAfterCompletionReturnsCachedResult: once a send has completed, later
// calls with the same key return the recorded result rather than sending.
func TestSendAfterCompletionReturnsCachedResult(t *testing.T) {
	base := &countingProvider{}
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	p := NewIdempotentProvider(base, &IdempotencyConfig{Store: store, TTL: time.Minute})
	msg := func() *Message {
		return NewMessage("test@example.com").WithBody("x").WithIdempotencyKey("done-key")
	}

	first, err := p.Send(context.Background(), msg())
	if err != nil || first == nil || !first.OK {
		t.Fatalf("first Send = (%v, %v)", first, err)
	}

	second, err := p.Send(context.Background(), msg())
	if err != nil {
		t.Fatalf("second Send error = %v", err)
	}
	if second == nil || !second.OK {
		t.Fatalf("second Send = %v, want the recorded result", second)
	}
	if got := atomic.LoadInt32(&base.sends); got != 1 {
		t.Errorf("provider called %d times, want 1", got)
	}

	// The recorded result must not be aliased: mutating what one caller got
	// must not change what the next caller sees.
	second.OK = false
	third, _ := p.Send(context.Background(), msg())
	if third == nil || !third.OK {
		t.Error("mutating a returned result corrupted the stored one")
	}
}

// TestAbandonReleasesClaim: a send that fails without producing a result must
// not block retries for the whole TTL.
func TestAbandonReleasesClaim(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	claimed, existing, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || !claimed || existing != nil {
		t.Fatalf("Reserve = (%v, %v, %v), want a fresh claim", claimed, existing, err)
	}

	claimed2, existing2, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || claimed2 || existing2 != nil {
		t.Fatalf("second Reserve = (%v, %v, %v), want claimed=false with no result", claimed2, existing2, err)
	}

	if err := store.Abandon(ctx, "k"); err != nil {
		t.Fatalf("Abandon() error = %v", err)
	}
	claimed3, _, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || !claimed3 {
		t.Errorf("Reserve after Abandon = (%v, %v), want a fresh claim", claimed3, err)
	}
}

// TestStoreCloseStopsCleanupGoroutine: every store used to leak its cleanup
// goroutine, which also kept the store itself alive.
func TestStoreCloseStopsCleanupGoroutine(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// Close must be safe to call more than once.
	if err := store.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}
