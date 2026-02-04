package provider

import (
	"context"
	"testing"
)

func TestDefaultSMTPConfig(t *testing.T) {
	config := DefaultSMTPConfig()

	if config.Port != 587 {
		t.Errorf("DefaultSMTPConfig() Port = %v, want 587", config.Port)
	}
	if config.Timeout == 0 {
		t.Error("DefaultSMTPConfig() Timeout should not be zero")
	}
	if !config.UseStartTLS {
		t.Error("DefaultSMTPConfig() UseStartTLS should be true")
	}
}

func TestSMTPConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *SMTPConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "noreply@example.com",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &SMTPConfig{
				Port: 587,
				From: "noreply@example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 0,
				From: "noreply@example.com",
			},
			wantErr: true,
		},
		{
			name: "port too high",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 70000,
				From: "noreply@example.com",
			},
			wantErr: true,
		},
		{
			name: "missing from",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
			},
			wantErr: true,
		},
		{
			name: "from with newline",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "noreply@example.com\r\nBcc:bad@example.com",
			},
			wantErr: true,
		},
		{
			name: "from name with newline",
			config: &SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				From:     "noreply@example.com",
				FromName: "Sender\r\nBcc:bad@example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSMTPProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  *SMTPConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "noreply@example.com",
			},
			wantErr: false,
		},
		{
			name:    "nil config uses default",
			config:  nil,
			wantErr: true, // Default config has empty host
		},
		{
			name: "invalid config",
			config: &SMTPConfig{
				Host: "",
				Port: 587,
				From: "noreply@example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewSMTPProvider(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSMTPProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && provider == nil {
				t.Error("NewSMTPProvider() should return provider")
			}
		})
	}
}

func TestSMTPProvider_Channel(t *testing.T) {
	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}

func TestSMTPProvider_Name(t *testing.T) {
	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	if provider.Name() != "smtp" {
		t.Errorf("Name() = %v, want smtp", provider.Name())
	}
}

func TestNewSMTPProviderFromMap(t *testing.T) {
	configMap := map[string]string{
		"host":            "smtp.example.com",
		"port":            "587",
		"username":        "user",
		"password":        "pass",
		"from":            "noreply@example.com",
		"from_name":       "Test Sender",
		"use_tls":         "true",
		"use_starttls":    "false",
		"skip_tls_verify": "true",
	}

	provider, err := NewSMTPProviderFromMap(configMap)
	if err != nil {
		t.Fatalf("NewSMTPProviderFromMap() error = %v", err)
	}

	if provider.Channel() != ChannelEmail {
		t.Errorf("Channel() = %v, want email", provider.Channel())
	}
}

func TestSMTPProvider_Send_InvalidMessage(t *testing.T) {
	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	// Empty destination
	msg := NewMessage("")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for invalid message")
	}
	if result == nil || result.OK {
		t.Error("Send() should return failure result for invalid message")
	}
}

func TestSMTPProviderFactory(t *testing.T) {
	configMap := map[string]string{
		"host": "smtp.example.com",
		"port": "587",
		"from": "noreply@example.com",
	}

	provider, err := SMTPProviderFactory(configMap)
	if err != nil {
		t.Fatalf("SMTPProviderFactory() error = %v", err)
	}

	if provider.Name() != "smtp" {
		t.Errorf("Name() = %v, want smtp", provider.Name())
	}
}
