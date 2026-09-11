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
	sends int32
	delay time.Duration
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
	token, existing, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || token == "" || existing != nil {
		t.Fatalf("Reserve = (%q, %v, %v), want a fresh claim", token, existing, err)
	}

	token2, existing2, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || token2 != "" || existing2 != nil {
		t.Fatalf("second Reserve = (%q, %v, %v), want no claim and no result", token2, existing2, err)
	}

	if err := store.Abandon(ctx, "k", token); err != nil {
		t.Fatalf("Abandon() error = %v", err)
	}
	token3, _, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || token3 == "" {
		t.Errorf("Reserve after Abandon = (%q, %v), want a fresh claim", token3, err)
	}
}

// TestAbandonOnlyReleasesItsOwnClaim is the regression test for Abandon taking
// no owner identity. A send that overran its reservation TTL let a second
// caller claim the same key; the first caller's late Abandon then deleted that
// second, still-resultless claim, and a third caller walked straight into the
// provider alongside the second -- two messages for one idempotency key.
func TestAbandonOnlyReleasesItsOwnClaim(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Send A claims the key with a short TTL and overruns it.
	tokenA, _, err := store.Reserve(ctx, "k", 20*time.Millisecond)
	if err != nil || tokenA == "" {
		t.Fatalf("Reserve(A) = (%q, %v), want a claim", tokenA, err)
	}
	time.Sleep(40 * time.Millisecond)

	// Send B claims the now-expired key and is still running.
	tokenB, _, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || tokenB == "" {
		t.Fatalf("Reserve(B) = (%q, %v), want a claim after A's TTL expired", tokenB, err)
	}
	if tokenA == tokenB {
		t.Fatal("two reservations were handed the same token")
	}

	// A finally fails and abandons. This must not touch B's claim.
	if err := store.Abandon(ctx, "k", tokenA); err != nil {
		t.Fatalf("Abandon(A) error = %v", err)
	}

	tokenC, existing, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil {
		t.Fatalf("Reserve(C) error = %v", err)
	}
	if tokenC != "" {
		t.Error("a third caller claimed the key while B still held it -- A's Abandon released someone else's claim")
	}
	if existing != nil {
		t.Errorf("Reserve(C) returned a result %v, want none while B is in flight", existing)
	}

	// B can still release its own claim.
	if err := store.Abandon(ctx, "k", tokenB); err != nil {
		t.Fatalf("Abandon(B) error = %v", err)
	}
	if tokenD, _, err := store.Reserve(ctx, "k", time.Minute); err != nil || tokenD == "" {
		t.Errorf("Reserve after B abandoned = (%q, %v), want a fresh claim", tokenD, err)
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

// --- Codex review round 2 (PR #6) ---

// TestCachedResultIsDeeplyCopied is the regression test for copying only the
// outer struct. The result constructors allocate Metadata, so every caller
// that hit a cached key shared that MAP: one of them adding metadata mutated
// what the others saw, and raced with them while doing it.
func TestCachedResultIsDeeplyCopied(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	original := &SendResult{
		OK:       true,
		Provider: "p",
		Metadata: map[string]string{"attempt": "1"},
		Error:    &ProviderError{Message: "none"},
	}
	if err := store.Set(ctx, "k", original, time.Minute); err != nil {
		t.Fatal(err)
	}

	first, ok, err := store.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v, %v)", first, ok, err)
	}

	// A caller mutating what it was handed must not affect anyone else.
	first.Metadata["attempt"] = "999"
	first.Metadata["injected"] = "yes"
	first.Error.Message = "tampered"

	second, ok, err := store.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("second Get = (%v, %v, %v)", second, ok, err)
	}
	if got := second.Metadata["attempt"]; got != "1" {
		t.Errorf("Metadata[attempt] = %q after another caller mutated its copy, want 1", got)
	}
	if _, injected := second.Metadata["injected"]; injected {
		t.Error("a key added by another caller leaked into the cached result")
	}
	if second.Error.Message != "none" {
		t.Errorf("Error.Message = %q, want none: the error pointer is shared", second.Error.Message)
	}

	// Reserve returns the completed result on the same path.
	token, existing, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || token != "" || existing == nil {
		t.Fatalf("Reserve = (%q, %v, %v), want the recorded result", token, existing, err)
	}
	existing.Metadata["attempt"] = "777"
	if third, _, _ := store.Get(ctx, "k"); third.Metadata["attempt"] != "1" {
		t.Errorf("Metadata[attempt] = %q after mutating Reserve's result, want 1", third.Metadata["attempt"])
	}
}

// TestFinalizeOnlyRecordsItsOwnClaim is the regression test for recording an
// outcome with Set, which takes no token. A send that overran its reservation
// TTL could complete after a second caller had claimed the same key and
// replace that caller's entry -- overwriting a completed result, or erasing an
// in-flight claim -- so one idempotency key produced two different answers.
func TestFinalizeOnlyRecordsItsOwnClaim(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// A claims with a short TTL and overruns it.
	tokenA, _, err := store.Reserve(ctx, "k", 20*time.Millisecond)
	if err != nil || tokenA == "" {
		t.Fatalf("Reserve(A) = (%q, %v), want a claim", tokenA, err)
	}
	time.Sleep(40 * time.Millisecond)

	// B claims the expired key and completes.
	tokenB, _, err := store.Reserve(ctx, "k", time.Minute)
	if err != nil || tokenB == "" {
		t.Fatalf("Reserve(B) = (%q, %v), want a claim", tokenB, err)
	}
	if err := store.Finalize(ctx, "k", tokenB, &SendResult{OK: true, Provider: "B"}, time.Minute); err != nil {
		t.Fatalf("Finalize(B) error = %v", err)
	}

	// A finally completes. Its claim is long gone; B's answer must stand.
	err = store.Finalize(ctx, "k", tokenA, &SendResult{OK: true, Provider: "A"}, time.Minute)
	if !errors.Is(err, ErrClaimSuperseded) {
		t.Errorf("Finalize(A) error = %v, want ErrClaimSuperseded", err)
	}

	got, found, err := store.Get(ctx, "k")
	if err != nil || !found {
		t.Fatalf("Get = (%v, %v, %v), want B's recorded result", got, found, err)
	}
	if got.Provider != "B" {
		t.Errorf("recorded provider = %q, want B -- A's late Finalize overwrote it", got.Provider)
	}

	// And an in-flight claim must not be erased either.
	store2 := NewMemoryIdempotencyStore()
	defer func() { _ = store2.Close() }()
	tokenX, _, _ := store2.Reserve(ctx, "j", 20*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	tokenY, _, _ := store2.Reserve(ctx, "j", time.Minute)
	if err := store2.Finalize(ctx, "j", tokenX, &SendResult{OK: true, Provider: "X"}, time.Minute); !errors.Is(err, ErrClaimSuperseded) {
		t.Errorf("Finalize over an in-flight claim = %v, want ErrClaimSuperseded", err)
	}
	if _, existing, _ := store2.Reserve(ctx, "j", time.Minute); existing != nil {
		t.Error("a late Finalize published a result over a claim that was still in flight")
	}
	_ = store2.Abandon(ctx, "j", tokenY)
}

// TestStoredResultIsDetachedFromTheCaller is the regression test for handing
// the store the provider's own *SendResult and returning that same pointer.
// The caller could then mutate what was cached -- WithMetadata writes into the
// Metadata map the result constructors allocate -- and raced with readers
// cloning that same map.
func TestStoredResultIsDetachedFromTheCaller(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	defer func() { _ = store.Close() }()

	p := NewIdempotentProvider(&metadataProvider{}, &IdempotencyConfig{Store: store, TTL: time.Minute})
	msg := &Message{To: "a@example.org", IdempotencyKey: "k"}

	first, err := p.Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}

	// The caller mutates the result it was handed.
	first.Metadata["tampered"] = "yes"
	first.Provider = "tampered"

	second, err := p.Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := second.Metadata["tampered"]; ok {
		t.Error("a caller's mutation reached the cached result")
	}
	if second.Provider != "original" {
		t.Errorf("cached provider = %q, want original", second.Provider)
	}
}

// metadataProvider returns a result carrying a populated Metadata map.
type metadataProvider struct{}

func (metadataProvider) Channel() Channel { return ChannelEmail }
func (metadataProvider) Name() string     { return "metadata" }
func (metadataProvider) Validate() error  { return nil }
func (metadataProvider) Send(_ context.Context, _ *Message) (*SendResult, error) {
	return &SendResult{OK: true, Provider: "original", Metadata: map[string]string{"id": "1"}}, nil
}
