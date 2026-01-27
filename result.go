package provider

import "time"

// SendResult represents the result of a send operation
type SendResult struct {
	// OK indicates if the send was successful
	OK bool

	// MessageID is the provider's message ID (if available)
	MessageID string

	// Provider is the name of the provider that handled the request
	Provider string

	// Channel is the channel used for sending
	Channel Channel

	// Timestamp is when the message was sent
	Timestamp time.Time

	// Metadata contains additional result data from the provider
	Metadata map[string]string

	// Error contains error details if OK is false
	Error *ProviderError
}

// NewSuccessResult creates a successful send result
func NewSuccessResult(provider string, channel Channel, messageID string) *SendResult {
	return &SendResult{
		OK:        true,
		MessageID: messageID,
		Provider:  provider,
		Channel:   channel,
		Timestamp: time.Now(),
		Metadata:  make(map[string]string),
	}
}

// NewFailureResult creates a failed send result
func NewFailureResult(provider string, channel Channel, err *ProviderError) *SendResult {
	return &SendResult{
		OK:        false,
		Provider:  provider,
		Channel:   channel,
		Timestamp: time.Now(),
		Error:     err,
		Metadata:  make(map[string]string),
	}
}

// WithMetadata adds metadata to the result
func (r *SendResult) WithMetadata(key, value string) *SendResult {
	if r.Metadata == nil {
		r.Metadata = make(map[string]string)
	}
	r.Metadata[key] = value
	return r
}

// GetError returns the error if the result is a failure
func (r *SendResult) GetError() error {
	if r.OK {
		return nil
	}
	if r.Error != nil {
		return r.Error
	}
	return ErrSendFailed("unknown error", nil)
}
