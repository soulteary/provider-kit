package provider

import (
	"context"
	"fmt"
	"sync"
)

// Registry manages available providers
type Registry struct {
	mu        sync.RWMutex
	providers map[Channel]Provider
	factories map[string]ProviderFactory
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[Channel]Provider),
		factories: make(map[string]ProviderFactory),
	}
}

// Register registers a provider for its channel
func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return ErrValidationFailed("provider cannot be nil")
	}

	if err := provider.Validate(); err != nil {
		return fmt.Errorf("provider validation failed: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[provider.Channel()] = provider
	return nil
}

// RegisterFactory registers a provider factory by name
func (r *Registry) RegisterFactory(name string, factory ProviderFactory) error {
	if name == "" {
		return ErrValidationFailed("factory name cannot be empty")
	}
	if factory == nil {
		return ErrValidationFailed("factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[name] = factory
	return nil
}

// CreateProvider creates a provider using a registered factory
func (r *Registry) CreateProvider(name string, config map[string]string) (Provider, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		return nil, ErrValidationFailed(fmt.Sprintf("no factory registered for provider: %s", name))
	}

	return factory(config)
}

// Get returns a provider for the given channel
func (r *Registry) Get(channel Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[channel]
	if !ok {
		return nil, ErrNotRegistered(channel)
	}
	return provider, nil
}

// GetProvider is an alias for Get (for backward compatibility)
func (r *Registry) GetProvider(channel Channel) (Provider, error) {
	return r.Get(channel)
}

// Send sends a message using the appropriate provider for the channel
func (r *Registry) Send(ctx context.Context, channel Channel, msg *Message) (*SendResult, error) {
	provider, err := r.Get(channel)
	if err != nil {
		return nil, err
	}
	return provider.Send(ctx, msg)
}

// Unregister removes a provider for a channel
func (r *Registry) Unregister(channel Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, channel)
}

// UnregisterFactory removes a factory by name
func (r *Registry) UnregisterFactory(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.factories, name)
}

// Has checks if a provider is registered for a channel
func (r *Registry) Has(channel Channel) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[channel]
	return ok
}

// HasFactory checks if a factory is registered
func (r *Registry) HasFactory(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// Channels returns all registered channels
func (r *Registry) Channels() []Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channels := make([]Channel, 0, len(r.providers))
	for ch := range r.providers {
		channels = append(channels, ch)
	}
	return channels
}

// Providers returns all registered providers
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	return providers
}

// Clear removes all registered providers and factories
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = make(map[Channel]Provider)
	r.factories = make(map[string]ProviderFactory)
}

// Global registry instance
var globalRegistry = NewRegistry()

// GlobalRegistry returns the global registry instance
func GlobalRegistry() *Registry {
	return globalRegistry
}

// RegisterGlobal registers a provider in the global registry
func RegisterGlobal(provider Provider) error {
	return globalRegistry.Register(provider)
}

// GetGlobal gets a provider from the global registry
func GetGlobal(channel Channel) (Provider, error) {
	return globalRegistry.Get(channel)
}

// SendGlobal sends a message using the global registry
func SendGlobal(ctx context.Context, channel Channel, msg *Message) (*SendResult, error) {
	return globalRegistry.Send(ctx, channel, msg)
}
