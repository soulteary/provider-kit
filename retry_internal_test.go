package provider

import (
	"context"
	"testing"
	"time"
)

func TestRetrySender_Send_Success(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	})

	sender := NewRetrySender(registry, &RetryConfig{
		MaxRetries:        2,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	})

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := sender.Send(context.Background(), ChannelEmail, msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestRetrySender_Send_RetrySuccess(t *testing.T) {
	attempts := 0
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			if attempts < 2 {
				return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
			}
			return NewSuccessResult("mock", ChannelEmail, "msg-123"), nil
		},
	})

	sender := NewRetrySender(registry, &RetryConfig{
		MaxRetries:        3,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	})

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := sender.Send(context.Background(), ChannelEmail, msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() should succeed after retry")
	}
	if attempts != 2 {
		t.Errorf("Should have made 2 attempts, got %d", attempts)
	}
}

func TestRetrySender_Send_NonRetryableError(t *testing.T) {
	attempts := 0
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			return NewFailureResult("mock", ChannelEmail, ErrInvalidDestination("bad")), ErrInvalidDestination("bad")
		},
	})

	sender := NewRetrySender(registry, &RetryConfig{
		MaxRetries:        3,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	})

	msg := NewMessage("test@example.com").WithBody("Test")
	_, err := sender.Send(context.Background(), ChannelEmail, msg)
	if err == nil {
		t.Error("Send() should return error for non-retryable error")
	}
	if attempts != 1 {
		t.Errorf("Should have made only 1 attempt, got %d", attempts)
	}
}

func TestRetrySender_Send_AllRetriesExhausted(t *testing.T) {
	attempts := 0
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
		},
	})

	sender := NewRetrySender(registry, &RetryConfig{
		MaxRetries:        2,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	})

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := sender.Send(context.Background(), ChannelEmail, msg)
	if err == nil {
		t.Error("Send() should return error after all retries exhausted")
	}
	if result.OK {
		t.Error("Result should be failure")
	}
	if attempts != 3 { // 1 initial + 2 retries
		t.Errorf("Should have made 3 attempts, got %d", attempts)
	}
}

func TestRetrySender_Send_ContextCancelled(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
		},
	})

	sender := NewRetrySender(registry, &RetryConfig{
		MaxRetries:        10,
		RetryDelay:        100 * time.Millisecond,
		MaxRetryDelay:     1 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	msg := NewMessage("test@example.com").WithBody("Test")
	_, err := sender.Send(ctx, ChannelEmail, msg)
	if err != context.Canceled {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}
