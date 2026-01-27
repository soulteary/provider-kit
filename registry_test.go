package provider

import (
	"context"
	"testing"
)

// mockProvider is a test implementation of Provider
type mockProvider struct {
	channel   Channel
	name      string
	valid     bool
	sendError error
	sendFunc  func(ctx context.Context, msg *Message) (*SendResult, error)
}

func (m *mockProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, msg)
	}
	if m.sendError != nil {
		return NewFailureResult(m.name, m.channel, NormalizeError(m.sendError, m.channel, m.name)), m.sendError
	}
	return NewSuccessResult(m.name, m.channel, "mock-msg-id"), nil
}

func (m *mockProvider) Channel() Channel {
	return m.channel
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Validate() error {
	if !m.valid {
		return ErrValidationFailed("provider validation failed")
	}
	return nil
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if registry.providers == nil {
		t.Fatal("NewRegistry() providers map is nil")
	}
	if len(registry.providers) != 0 {
		t.Errorf("NewRegistry() providers map should be empty, got %d", len(registry.providers))
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		wantErr  bool
	}{
		{
			name: "valid provider",
			provider: &mockProvider{
				channel: ChannelEmail,
				name:    "mock",
				valid:   true,
			},
			wantErr: false,
		},
		{
			name: "invalid provider",
			provider: &mockProvider{
				channel: ChannelSMS,
				name:    "mock",
				valid:   false,
			},
			wantErr: true,
		},
		{
			name:     "nil provider",
			provider: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	registry := NewRegistry()

	provider1 := &mockProvider{
		channel: ChannelEmail,
		name:    "provider1",
		valid:   true,
	}

	provider2 := &mockProvider{
		channel: ChannelEmail,
		name:    "provider2",
		valid:   true,
	}

	if err := registry.Register(provider1); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := registry.Register(provider2); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Should overwrite the first provider
	p, err := registry.Get(ChannelEmail)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if p.Name() != "provider2" {
		t.Errorf("Get() should return provider2, got %v", p.Name())
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()

	emailProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	smsProvider := &mockProvider{
		channel: ChannelSMS,
		name:    "sms",
		valid:   true,
	}

	if err := registry.Register(emailProvider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := registry.Register(smsProvider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tests := []struct {
		name    string
		channel Channel
		wantErr bool
	}{
		{
			name:    "get email provider",
			channel: ChannelEmail,
			wantErr: false,
		},
		{
			name:    "get sms provider",
			channel: ChannelSMS,
			wantErr: false,
		},
		{
			name:    "get non-existent provider",
			channel: Channel("unknown"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := registry.Get(tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && provider == nil {
				t.Error("Get() returned nil provider")
			}
			if !tt.wantErr && provider.Channel() != tt.channel {
				t.Errorf("Get() channel = %v, want %v", provider.Channel(), tt.channel)
			}
		})
	}
}

func TestRegistry_Send(t *testing.T) {
	registry := NewRegistry()

	emailProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	if err := registry.Register(emailProvider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	msg := NewMessage("test@example.com").WithBody("Test message")

	// Test successful send
	result, err := registry.Send(context.Background(), ChannelEmail, msg)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
	if result == nil {
		t.Fatal("Send() result is nil")
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}

	// Test send to non-existent channel
	_, err = registry.Send(context.Background(), ChannelSMS, msg)
	if err == nil {
		t.Error("Send() should return error for non-existent channel")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	registry := NewRegistry()

	provider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	if err := registry.Register(provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !registry.Has(ChannelEmail) {
		t.Error("Has() should return true after register")
	}

	registry.Unregister(ChannelEmail)

	if registry.Has(ChannelEmail) {
		t.Error("Has() should return false after unregister")
	}
}

func TestRegistry_Has(t *testing.T) {
	registry := NewRegistry()

	provider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	if registry.Has(ChannelEmail) {
		t.Error("Has() should return false before register")
	}

	if err := registry.Register(provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !registry.Has(ChannelEmail) {
		t.Error("Has() should return true after register")
	}
}

func TestRegistry_Channels(t *testing.T) {
	registry := NewRegistry()

	emailProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	smsProvider := &mockProvider{
		channel: ChannelSMS,
		name:    "sms",
		valid:   true,
	}

	_ = registry.Register(emailProvider)
	_ = registry.Register(smsProvider)

	channels := registry.Channels()
	if len(channels) != 2 {
		t.Errorf("Channels() returned %d, want 2", len(channels))
	}
}

func TestRegistry_Providers(t *testing.T) {
	registry := NewRegistry()

	emailProvider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	_ = registry.Register(emailProvider)

	providers := registry.Providers()
	if len(providers) != 1 {
		t.Errorf("Providers() returned %d, want 1", len(providers))
	}
}

func TestRegistry_Clear(t *testing.T) {
	registry := NewRegistry()

	provider := &mockProvider{
		channel: ChannelEmail,
		name:    "email",
		valid:   true,
	}

	_ = registry.Register(provider)
	registry.Clear()

	if len(registry.Channels()) != 0 {
		t.Error("Clear() should remove all providers")
	}
}

func TestRegistry_Factory(t *testing.T) {
	registry := NewRegistry()

	factory := func(config map[string]string) (Provider, error) {
		return &mockProvider{
			channel: ChannelEmail,
			name:    config["name"],
			valid:   true,
		}, nil
	}

	// Register factory
	if err := registry.RegisterFactory("mock", factory); err != nil {
		t.Fatalf("RegisterFactory() error = %v", err)
	}

	if !registry.HasFactory("mock") {
		t.Error("HasFactory() should return true")
	}

	// Create provider from factory
	provider, err := registry.CreateProvider("mock", map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	if provider.Name() != "test" {
		t.Errorf("CreateProvider() provider.Name() = %v, want test", provider.Name())
	}

	// Unregister factory
	registry.UnregisterFactory("mock")

	if registry.HasFactory("mock") {
		t.Error("HasFactory() should return false after unregister")
	}
}

func TestRegistry_Factory_Errors(t *testing.T) {
	registry := NewRegistry()

	// Empty name
	if err := registry.RegisterFactory("", nil); err == nil {
		t.Error("RegisterFactory() should return error for empty name")
	}

	// Nil factory
	if err := registry.RegisterFactory("test", nil); err == nil {
		t.Error("RegisterFactory() should return error for nil factory")
	}

	// Create from non-existent factory
	_, err := registry.CreateProvider("nonexistent", nil)
	if err == nil {
		t.Error("CreateProvider() should return error for non-existent factory")
	}
}

func TestGlobalRegistry(t *testing.T) {
	// Clear global registry first
	GlobalRegistry().Clear()

	provider := &mockProvider{
		channel: ChannelEmail,
		name:    "global-test",
		valid:   true,
	}

	// Test RegisterGlobal
	if err := RegisterGlobal(provider); err != nil {
		t.Fatalf("RegisterGlobal() error = %v", err)
	}

	// Test GetGlobal
	p, err := GetGlobal(ChannelEmail)
	if err != nil {
		t.Fatalf("GetGlobal() error = %v", err)
	}
	if p.Name() != "global-test" {
		t.Errorf("GetGlobal() provider.Name() = %v, want global-test", p.Name())
	}

	// Test SendGlobal
	msg := NewMessage("test@example.com").WithBody("Test")
	result, err := SendGlobal(context.Background(), ChannelEmail, msg)
	if err != nil {
		t.Fatalf("SendGlobal() error = %v", err)
	}
	if !result.OK {
		t.Error("SendGlobal() result.OK should be true")
	}

	// Clean up
	GlobalRegistry().Clear()
}
