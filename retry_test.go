package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("DefaultRetryConfig() MaxRetries = %v, want 3", config.MaxRetries)
	}
	if config.RetryDelay == 0 {
		t.Error("DefaultRetryConfig() RetryDelay should not be zero")
	}
	if config.MaxRetryDelay == 0 {
		t.Error("DefaultRetryConfig() MaxRetryDelay should not be zero")
	}
	if config.BackoffMultiplier == 0 {
		t.Error("DefaultRetryConfig() BackoffMultiplier should not be zero")
	}
	if len(config.RetryableReasons) == 0 {
		t.Error("DefaultRetryConfig() RetryableReasons should not be empty")
	}
}

func TestRetryConfig_IsRetryable(t *testing.T) {
	config := DefaultRetryConfig()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "retryable provider down",
			err:  ErrProviderDown("down", nil),
			want: true,
		},
		{
			name: "retryable timeout",
			err:  ErrTimeout("timeout", nil),
			want: true,
		},
		{
			name: "retryable rate limited",
			err:  ErrRateLimited("limited"),
			want: true,
		},
		{
			name: "non-retryable invalid config",
			err:  ErrInvalidConfig("invalid"),
			want: false,
		},
		{
			name: "non-retryable invalid destination",
			err:  ErrInvalidDestination("invalid"),
			want: false,
		},
		{
			name: "standard error (network)",
			err:  errors.New("connection refused"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryConfig_CalculateDelay(t *testing.T) {
	config := &RetryConfig{
		RetryDelay:        100 * time.Millisecond,
		MaxRetryDelay:     1 * time.Second,
		BackoffMultiplier: 2.0,
	}

	tests := []struct {
		attempt  int
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{0, 100 * time.Millisecond, 500 * time.Millisecond},
		{1, 200 * time.Millisecond, 1 * time.Second},
		{2, 400 * time.Millisecond, 1 * time.Second},
		{10, 0, 1 * time.Second}, // Should be capped at max
	}

	for _, tt := range tests {
		delay := config.CalculateDelay(tt.attempt)
		if delay > tt.maxDelay {
			t.Errorf("CalculateDelay(%d) = %v, should not exceed %v", tt.attempt, delay, tt.maxDelay)
		}
	}
}

func TestNewRetryProvider(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	// Test with nil config
	provider := NewRetryProvider(baseProvider, nil)
	if provider == nil {
		t.Fatal("NewRetryProvider() returned nil")
	}

	// Test with custom config
	config := &RetryConfig{
		MaxRetries:        5,
		RetryDelay:        50 * time.Millisecond,
		MaxRetryDelay:     500 * time.Millisecond,
		BackoffMultiplier: 1.5,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	}
	provider = NewRetryProvider(baseProvider, config)
	if provider == nil {
		t.Fatal("NewRetryProvider() returned nil")
	}
}

func TestRetryProvider_Send_Success(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewRetryProvider(baseProvider, nil)

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestRetryProvider_Send_RetrySuccess(t *testing.T) {
	attempts := 0
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			if attempts < 3 {
				// Fail first 2 attempts with retryable error
				return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
			}
			return NewSuccessResult("mock", ChannelEmail, "msg-123"), nil
		},
	}

	config := &RetryConfig{
		MaxRetries:        3,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	}

	provider := NewRetryProvider(baseProvider, config)

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true after retries")
	}
	if attempts != 3 {
		t.Errorf("Should have made 3 attempts, got %d", attempts)
	}
}

func TestRetryProvider_Send_NonRetryableError(t *testing.T) {
	attempts := 0
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			// Non-retryable error
			return NewFailureResult("mock", ChannelEmail, ErrInvalidDestination("invalid")), ErrInvalidDestination("invalid")
		},
	}

	config := &RetryConfig{
		MaxRetries:        3,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	}

	provider := NewRetryProvider(baseProvider, config)

	msg := NewMessage("test@example.com").WithBody("Test")
	_, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for non-retryable error")
	}
	if attempts != 1 {
		t.Errorf("Should have made only 1 attempt, got %d", attempts)
	}
}

func TestRetryProvider_Send_AllRetriesExhausted(t *testing.T) {
	attempts := 0
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			attempts++
			return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
		},
	}

	config := &RetryConfig{
		MaxRetries:        2,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	}

	provider := NewRetryProvider(baseProvider, config)

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error after all retries exhausted")
	}
	if result.OK {
		t.Error("Send() result.OK should be false after all retries exhausted")
	}
	if attempts != 3 { // 1 initial + 2 retries
		t.Errorf("Should have made 3 attempts, got %d", attempts)
	}
}

func TestRetryProvider_Send_ContextCancelled(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
		sendFunc: func(ctx context.Context, msg *Message) (*SendResult, error) {
			return NewFailureResult("mock", ChannelEmail, ErrProviderDown("down", nil)), ErrProviderDown("down", nil)
		},
	}

	config := &RetryConfig{
		MaxRetries:        10,
		RetryDelay:        100 * time.Millisecond,
		MaxRetryDelay:     1 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableReasons:  []ErrorReason{ReasonProviderDown},
	}

	provider := NewRetryProvider(baseProvider, config)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	msg := NewMessage("test@example.com").WithBody("Test")
	_, err := provider.Send(ctx, msg)
	if err != context.Canceled {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}

func TestRetryProvider_Channel(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewRetryProvider(baseProvider, nil)

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}

func TestRetryProvider_Name(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewRetryProvider(baseProvider, nil)

	if provider.Name() != "mock" {
		t.Errorf("Name() = %v, want mock", provider.Name())
	}
}

func TestRetryProvider_Validate(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := NewRetryProvider(baseProvider, nil)

	if err := provider.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestWrapWithRetry(t *testing.T) {
	baseProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	}

	provider := WrapWithRetry(baseProvider, 3, 100*time.Millisecond)

	if provider == nil {
		t.Fatal("WrapWithRetry() returned nil")
	}

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}

func TestNewRetrySender(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&mockProvider{
		channel: ChannelEmail,
		name:    "mock",
		valid:   true,
	})

	sender := NewRetrySender(registry, nil)
	if sender == nil {
		t.Fatal("NewRetrySender() returned nil")
	}

	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := sender.Send(context.Background(), ChannelEmail, msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}
