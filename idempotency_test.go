package provider

import (
	"context"
	"testing"
	"time"
)

func TestNewMemoryIdempotencyStore(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	if store == nil {
		t.Fatal("NewMemoryIdempotencyStore() returned nil")
	}
	if store.entries == nil {
		t.Error("NewMemoryIdempotencyStore() entries should not be nil")
	}
}

func TestMemoryIdempotencyStore_GetSet(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	ctx := context.Background()

	result := NewSuccessResult("test", ChannelEmail, "msg-123")

	// Test Set
	err := store.Set(ctx, "key1", result, time.Minute)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Test Get
	got, found, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() should find the key")
	}
	if got.MessageID != result.MessageID {
		t.Errorf("Get() MessageID = %v, want %v", got.MessageID, result.MessageID)
	}

	// Test Get non-existent
	_, found, err = store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found {
		t.Error("Get() should not find non-existent key")
	}
}

func TestMemoryIdempotencyStore_Delete(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	ctx := context.Background()

	result := NewSuccessResult("test", ChannelEmail, "msg-123")

	_ = store.Set(ctx, "key1", result, time.Minute)

	err := store.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, found, _ := store.Get(ctx, "key1")
	if found {
		t.Error("Get() should not find deleted key")
	}
}

func TestMemoryIdempotencyStore_Expiration(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	ctx := context.Background()

	result := NewSuccessResult("test", ChannelEmail, "msg-123")

	// Set with very short TTL
	_ = store.Set(ctx, "key1", result, time.Millisecond)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, found, _ := store.Get(ctx, "key1")
	if found {
		t.Error("Get() should not find expired key")
	}
}

func TestMemoryIdempotencyStore_RemoveExpired(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	ctx := context.Background()

	result := NewSuccessResult("test", ChannelEmail, "msg-123")

	// Set with very short TTL
	_ = store.Set(ctx, "key1", result, time.Millisecond)
	_ = store.Set(ctx, "key2", result, time.Hour)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Manually trigger cleanup
	store.removeExpired()

	// key1 should be removed
	store.mu.RLock()
	_, key1Exists := store.entries["key1"]
	_, key2Exists := store.entries["key2"]
	store.mu.RUnlock()

	if key1Exists {
		t.Error("Expired key1 should be removed")
	}
	if !key2Exists {
		t.Error("Non-expired key2 should still exist")
	}
}

func TestDefaultIdempotencyConfig(t *testing.T) {
	config := DefaultIdempotencyConfig()

	if config.Store == nil {
		t.Error("DefaultIdempotencyConfig() Store should not be nil")
	}
	if config.TTL == 0 {
		t.Error("DefaultIdempotencyConfig() TTL should not be zero")
	}
}

func TestNewIdempotentProvider(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	// Test with nil config
	provider := NewIdempotentProvider(baseProvider, nil)
	if provider == nil {
		t.Fatal("NewIdempotentProvider() returned nil")
	}

	// Test with custom config
	config := &IdempotencyConfig{
		Store: NewMemoryIdempotencyStore(),
		TTL:   10 * time.Minute,
	}
	provider = NewIdempotentProvider(baseProvider, config)
	if provider == nil {
		t.Fatal("NewIdempotentProvider() returned nil")
	}
}

func TestIdempotentProvider_Send_WithoutKey(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, nil)

	// Message without idempotency key should go through directly
	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestIdempotentProvider_Send_WithKey_Cached(t *testing.T) {
	sendCount := 0
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			sendCount++
			return NewSuccessResult("mock", ChannelEmail, "msg-123"), nil
		},
	}

	provider := NewIdempotentProvider(baseProvider, nil)

	msg := NewMessage("test@example.com").
		WithBody("Test").
		WithIdempotencyKey("idem-key-1")

	// First send
	result1, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result1.OK {
		t.Error("First send result.OK should be true")
	}

	// Second send with same key should return cached result
	result2, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result2.OK {
		t.Error("Second send result.OK should be true")
	}

	// Should only have called base provider once
	if sendCount != 1 {
		t.Errorf("Base provider should be called once, got %d", sendCount)
	}

	// Results should be the same
	if result1.MessageID != result2.MessageID {
		t.Error("Cached result should have same MessageID")
	}
}

func TestIdempotentProvider_Channel(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, nil)

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}

func TestIdempotentProvider_Name(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, nil)

	if provider.Name() != "mock" {
		t.Errorf("Name() = %v, want mock", provider.Name())
	}
}

func TestIdempotentProvider_Validate(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, nil)

	if err := provider.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestWrapWithIdempotency(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	store := NewMemoryIdempotencyStore()
	provider := WrapWithIdempotency(baseProvider, store, 5*time.Minute)

	if provider == nil {
		t.Fatal("WrapWithIdempotency() returned nil")
	}

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}
