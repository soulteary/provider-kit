package provider

import (
	"errors"
	"fmt"
)

// Error reason codes for provider operations
type ErrorReason string

const (
	// ReasonSendFailed indicates the send operation failed
	ReasonSendFailed ErrorReason = "send_failed"
	// ReasonProviderDown indicates the provider is unavailable
	ReasonProviderDown ErrorReason = "provider_down"
	// ReasonInvalidConfig indicates invalid provider configuration
	ReasonInvalidConfig ErrorReason = "invalid_config"
	// ReasonRateLimited indicates the provider rate limited the request
	ReasonRateLimited ErrorReason = "rate_limited"
	// ReasonInvalidDestination indicates the destination is invalid
	ReasonInvalidDestination ErrorReason = "invalid_destination"
	// ReasonTimeout indicates the request timed out
	ReasonTimeout ErrorReason = "timeout"
	// ReasonUnauthorized indicates authentication failed
	ReasonUnauthorized ErrorReason = "unauthorized"
	// ReasonNotRegistered indicates no provider is registered for the channel
	ReasonNotRegistered ErrorReason = "not_registered"
	// ReasonValidationFailed indicates provider validation failed
	ReasonValidationFailed ErrorReason = "validation_failed"
	// ReasonIdempotencyConflict indicates an idempotency conflict
	ReasonIdempotencyConflict ErrorReason = "idempotency_conflict"
)

// String returns the string representation of the error reason
func (r ErrorReason) String() string {
	return string(r)
}

// ProviderError represents a provider operation error
type ProviderError struct {
	Reason       ErrorReason
	Message      string
	ProviderName string
	Channel      Channel
	Err          error // Underlying error
}

// Error implements the error interface
func (e *ProviderError) Error() string {
	if e.ProviderName != "" {
		return fmt.Sprintf("[%s/%s] %s: %s", e.Channel, e.ProviderName, e.Reason, e.Message)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Channel, e.Reason, e.Message)
}

// Unwrap returns the underlying error
func (e *ProviderError) Unwrap() error {
	return e.Err
}

// NewProviderError creates a new ProviderError
func NewProviderError(reason ErrorReason, message string) *ProviderError {
	return &ProviderError{
		Reason:  reason,
		Message: message,
	}
}

// WithProvider adds provider information to the error
func (e *ProviderError) WithProvider(name string, channel Channel) *ProviderError {
	e.ProviderName = name
	e.Channel = channel
	return e
}

// WithError adds an underlying error
func (e *ProviderError) WithError(err error) *ProviderError {
	e.Err = err
	return e
}

// Common error constructors

// ErrSendFailed creates a send failure error
func ErrSendFailed(message string, err error) *ProviderError {
	return &ProviderError{
		Reason:  ReasonSendFailed,
		Message: message,
		Err:     err,
	}
}

// ErrProviderDown creates a provider unavailable error
func ErrProviderDown(message string, err error) *ProviderError {
	return &ProviderError{
		Reason:  ReasonProviderDown,
		Message: message,
		Err:     err,
	}
}

// ErrInvalidConfig creates an invalid configuration error
func ErrInvalidConfig(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonInvalidConfig,
		Message: message,
	}
}

// ErrRateLimited creates a rate limited error
func ErrRateLimited(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonRateLimited,
		Message: message,
	}
}

// ErrInvalidDestination creates an invalid destination error
func ErrInvalidDestination(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonInvalidDestination,
		Message: message,
	}
}

// ErrTimeout creates a timeout error
func ErrTimeout(message string, err error) *ProviderError {
	return &ProviderError{
		Reason:  ReasonTimeout,
		Message: message,
		Err:     err,
	}
}

// ErrUnauthorized creates an unauthorized error
func ErrUnauthorized(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonUnauthorized,
		Message: message,
	}
}

// ErrNotRegistered creates a not registered error
func ErrNotRegistered(channel Channel) *ProviderError {
	return &ProviderError{
		Reason:  ReasonNotRegistered,
		Message: fmt.Sprintf("no provider registered for channel: %s", channel),
		Channel: channel,
	}
}

// ErrValidationFailed creates a validation failed error
func ErrValidationFailed(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonValidationFailed,
		Message: message,
	}
}

// ErrIdempotencyConflict creates an idempotency conflict error
func ErrIdempotencyConflict(message string) *ProviderError {
	return &ProviderError{
		Reason:  ReasonIdempotencyConflict,
		Message: message,
	}
}

// IsProviderError checks if the error is a ProviderError
func IsProviderError(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr)
}

// GetErrorReason extracts the error reason from a ProviderError
func GetErrorReason(err error) (ErrorReason, bool) {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Reason, true
	}
	return "", false
}

// NormalizeError normalizes any error to a ProviderError
func NormalizeError(err error, channel Channel, providerName string) *ProviderError {
	if err == nil {
		return nil
	}

	// Already a ProviderError
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.Channel == "" {
			providerErr.Channel = channel
		}
		if providerErr.ProviderName == "" {
			providerErr.ProviderName = providerName
		}
		return providerErr
	}

	// Wrap unknown error as send failure
	return &ProviderError{
		Reason:       ReasonSendFailed,
		Message:      err.Error(),
		ProviderName: providerName,
		Channel:      channel,
		Err:          err,
	}
}
