package provider

import (
	"testing"
)

func TestRegistry_GetProvider_Alias(t *testing.T) {
	registry := NewRegistry()

	provider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	if err := registry.Register(provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test GetProvider (alias for Get)
	p, err := registry.GetProvider(ChannelEmail)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if p.Name() != "email" {
		t.Errorf("GetProvider() Name = %v, want email", p.Name())
	}

	// Test GetProvider for non-existent
	_, err = registry.GetProvider(ChannelSMS)
	if err == nil {
		t.Error("GetProvider() should return error for non-existent channel")
	}
}
