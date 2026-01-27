package provider

import "context"

// Provider is the interface for message providers
type Provider interface {
	// Send sends a message via the provider
	Send(ctx context.Context, msg *Message) (*SendResult, error)

	// Channel returns the channel type this provider supports
	Channel() Channel

	// Name returns the provider's name (e.g., "smtp", "aliyun", "twilio")
	Name() string

	// Validate checks if the provider is properly configured
	Validate() error
}

// ProviderFactory creates a provider instance
type ProviderFactory func(config map[string]string) (Provider, error)

// Sender is a simplified interface for sending messages
type Sender interface {
	// Send sends a message using the appropriate provider
	Send(ctx context.Context, channel Channel, msg *Message) (*SendResult, error)
}

// MultiChannelProvider is a provider that supports multiple channels
type MultiChannelProvider interface {
	Provider
	// Channels returns all channels this provider supports
	Channels() []Channel
}
