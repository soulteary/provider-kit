package provider

import (
	"context"
	"time"
)

// RetryConfig contains retry configuration
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int
	// RetryDelay is the initial delay between retries
	RetryDelay time.Duration
	// MaxRetryDelay is the maximum delay between retries
	MaxRetryDelay time.Duration
	// BackoffMultiplier is the multiplier for exponential backoff
	BackoffMultiplier float64
	// RetryableReasons are error reasons that should trigger a retry
	RetryableReasons []ErrorReason
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:        3,
		RetryDelay:        100 * time.Millisecond,
		MaxRetryDelay:     5 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableReasons: []ErrorReason{
			ReasonProviderDown,
			ReasonTimeout,
			ReasonRateLimited,
		},
	}
}

// IsRetryable checks if an error should trigger a retry
func (c *RetryConfig) IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	reason, ok := GetErrorReason(err)
	if !ok {
		// Network errors are generally retryable
		return true
	}

	for _, r := range c.RetryableReasons {
		if reason == r {
			return true
		}
	}

	return false
}

// CalculateDelay calculates the delay for the next retry attempt using exponential backoff
func (c *RetryConfig) CalculateDelay(attempt int) time.Duration {
	delay := time.Duration(float64(c.RetryDelay) * float64(attempt+1) * c.BackoffMultiplier)
	if delay > c.MaxRetryDelay {
		delay = c.MaxRetryDelay
	}
	return delay
}

// RetryProvider wraps a provider with retry support
type RetryProvider struct {
	provider Provider
	config   *RetryConfig
}

// NewRetryProvider wraps a provider with retry support
func NewRetryProvider(provider Provider, config *RetryConfig) *RetryProvider {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryProvider{
		provider: provider,
		config:   config,
	}
}

// Send sends a message with retry support
func (p *RetryProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	var lastErr error
	var lastResult *SendResult

	maxAttempts := p.config.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Calculate delay before retry
			delay := p.config.CalculateDelay(attempt - 1)

			// Wait before retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Attempt to send
		result, err := p.provider.Send(ctx, msg)
		lastResult = result
		lastErr = err

		if err == nil && result.OK {
			// Success
			return result, nil
		}

		// Check if error is retryable
		if err != nil && !p.config.IsRetryable(err) {
			// Non-retryable error
			return result, err
		}

		// Continue to next attempt
	}

	// All retries exhausted
	return lastResult, lastErr
}

// Channel returns the channel type
func (p *RetryProvider) Channel() Channel {
	return p.provider.Channel()
}

// Name returns the provider name
func (p *RetryProvider) Name() string {
	return p.provider.Name()
}

// Validate checks if the provider is properly configured
func (p *RetryProvider) Validate() error {
	return p.provider.Validate()
}

// WrapWithRetry wraps a provider with retry support
func WrapWithRetry(provider Provider, maxRetries int, retryDelay time.Duration) Provider {
	return NewRetryProvider(provider, &RetryConfig{
		MaxRetries:        maxRetries,
		RetryDelay:        retryDelay,
		MaxRetryDelay:     5 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableReasons:  DefaultRetryConfig().RetryableReasons,
	})
}

// RetrySender wraps a Sender with retry support
type RetrySender struct {
	sender Sender
	config *RetryConfig
}

// NewRetrySender wraps a sender with retry support
func NewRetrySender(sender Sender, config *RetryConfig) *RetrySender {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetrySender{
		sender: sender,
		config: config,
	}
}

// Send sends a message with retry support
func (s *RetrySender) Send(ctx context.Context, channel Channel, msg *Message) (*SendResult, error) {
	var lastErr error
	var lastResult *SendResult

	maxAttempts := s.config.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Calculate delay before retry
			delay := s.config.CalculateDelay(attempt - 1)

			// Wait before retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Attempt to send
		result, err := s.sender.Send(ctx, channel, msg)
		lastResult = result
		lastErr = err

		if err == nil && result.OK {
			// Success
			return result, nil
		}

		// Check if error is retryable
		if err != nil && !s.config.IsRetryable(err) {
			// Non-retryable error
			return result, err
		}

		// Continue to next attempt
	}

	// All retries exhausted
	return lastResult, lastErr
}
