package provider

import (
	"context"
	"testing"
	"time"
)

func TestNewIdempotentProvider_NilStore(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	// Config with nil store should create a default store
	config := &IdempotencyConfig{
		Store: nil,
		TTL:   5 * time.Minute,
	}
	provider := NewIdempotentProvider(baseProvider, config)

	if provider.store == nil {
		t.Error("Store should not be nil after NewIdempotentProvider")
	}
}

func TestNewIdempotentProvider_ZeroTTL(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	// Config with zero TTL should use default
	config := &IdempotencyConfig{
		Store: NewMemoryIdempotencyStore(),
		TTL:   0,
	}
	provider := NewIdempotentProvider(baseProvider, config)

	if provider.ttl == 0 {
		t.Error("TTL should not be zero after NewIdempotentProvider")
	}
}

func TestIdempotentProvider_Send_FailureResult(t *testing.T) {
	// Provider that always fails
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			err := ErrProviderDown("service unavailable", nil)
			return NewFailureResult("mock", ChannelEmail, err), err
		},
	}

	provider := NewIdempotentProvider(baseProvider, &IdempotencyConfig{
		Store: NewMemoryIdempotencyStore(),
		TTL:   5 * time.Minute,
	})

	msg := NewMessage("test@example.com").
		WithBody("Test").
		WithIdempotencyKey("fail-key-1")

	// First call should fail and cache the failure
	result1, err1 := provider.Send(context.Background(), msg)
	if err1 == nil {
		t.Error("First send should fail")
	}
	if result1.OK {
		t.Error("First result should be failure")
	}

	// Second call should return cached failure
	result2, _ := provider.Send(context.Background(), msg)
	if result2.OK {
		t.Error("Cached result should also be failure")
	}
}

func TestIdempotentProvider_CacheError(t *testing.T) {
	// Use a store that fails on Set (simulate cache failure)
	failingStore := &failingIdempotencyStore{}

	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, &IdempotencyConfig{
		Store: failingStore,
		TTL:   5 * time.Minute,
	})

	msg := NewMessage("test@example.com").
		WithBody("Test").
		WithIdempotencyKey("key-with-cache-fail")

	// Should still succeed even if cache fails
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send should succeed even if cache fails")
	}
}

// failingIdempotencyStore always fails on Set
type failingIdempotencyStore struct{}

func (s *failingIdempotencyStore) Get(ctx context.Context, key string) (*SendResult, bool, error) {
	return nil, false, nil
}

func (s *failingIdempotencyStore) Set(ctx context.Context, key string, result *SendResult, ttl time.Duration) error {
	return ErrSendFailed("cache write failed", nil)
}

func (s *failingIdempotencyStore) Delete(ctx context.Context, key string) error {
	return nil
}

func TestIdempotentProvider_Send_GetError(t *testing.T) {
	// Store that returns error on Get
	errorStore := &errorOnGetStore{}

	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewIdempotentProvider(baseProvider, &IdempotencyConfig{
		Store: errorStore,
		TTL:   5 * time.Minute,
	})

	msg := NewMessage("test@example.com").
		WithBody("Test").
		WithIdempotencyKey("error-key")

	// Should proceed with send even if cache Get fails
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send should succeed even if cache Get fails")
	}
}

type errorOnGetStore struct{}

func (s *errorOnGetStore) Get(ctx context.Context, key string) (*SendResult, bool, error) {
	return nil, false, ErrSendFailed("cache read failed", nil)
}

func (s *errorOnGetStore) Set(ctx context.Context, key string, result *SendResult, ttl time.Duration) error {
	return nil
}

func (s *errorOnGetStore) Delete(ctx context.Context, key string) error {
	return nil
}
