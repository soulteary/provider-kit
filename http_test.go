package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDefaultHTTPConfig(t *testing.T) {
	config := DefaultHTTPConfig()

	if config.SendEndpoint != "/v1/send" {
		t.Errorf("DefaultHTTPConfig() SendEndpoint = %v, want /v1/send", config.SendEndpoint)
	}
	if config.APIKeyHeader != "X-API-Key" {
		t.Errorf("DefaultHTTPConfig() APIKeyHeader = %v, want X-API-Key", config.APIKeyHeader)
	}
	if config.Timeout == 0 {
		t.Error("DefaultHTTPConfig() Timeout should not be zero")
	}
}

func TestHTTPConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *HTTPConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &HTTPConfig{
				BaseURL: "https://api.example.com",
			},
			wantErr: false,
		},
		{
			name: "missing base URL",
			config: &HTTPConfig{
				BaseURL: "",
			},
			wantErr: true,
		},
		{
			name: "invalid base URL",
			config: &HTTPConfig{
				BaseURL: "://bad-url",
			},
			wantErr: true,
		},
		{
			name: "missing scheme",
			config: &HTTPConfig{
				BaseURL: "api.example.com",
			},
			wantErr: true,
		},
		{
			name: "unsupported scheme",
			config: &HTTPConfig{
				BaseURL: "ftp://api.example.com",
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

func TestNewHTTPProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  *HTTPConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &HTTPConfig{
				BaseURL: "https://api.example.com",
			},
			wantErr: false,
		},
		{
			name:    "nil config uses default",
			config:  nil,
			wantErr: true, // Default config has empty base URL
		},
		{
			name: "invalid config",
			config: &HTTPConfig{
				BaseURL: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewHTTPProvider(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHTTPProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && provider == nil {
				t.Error("NewHTTPProvider() should return provider")
			}
		})
	}
}

func TestHTTPProvider_Channel(t *testing.T) {
	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:     "https://api.example.com",
		ChannelType: ChannelSMS,
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	if provider.Channel() != ChannelSMS {
		t.Errorf("Channel() = %v, want sms", provider.Channel())
	}
}

func TestHTTPProvider_Name(t *testing.T) {
	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "https://api.example.com",
		ProviderName: "custom-provider",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	if provider.Name() != "custom-provider" {
		t.Errorf("Name() = %v, want custom-provider", provider.Name())
	}
}

func TestResolveSendURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		endpoint string
		want     string
	}{
		{
			name:     "relative endpoint keeps base path",
			base:     "https://api.example.com/api/v2",
			endpoint: "v1/send",
			want:     "https://api.example.com/api/v2/v1/send",
		},
		{
			name:     "absolute endpoint keeps base path",
			base:     "https://api.example.com/api/v2",
			endpoint: "/v1/send",
			want:     "https://api.example.com/api/v2/v1/send",
		},
		{
			name:     "base root with absolute endpoint",
			base:     "https://api.example.com",
			endpoint: "/v1/send",
			want:     "https://api.example.com/v1/send",
		},
		{
			name:     "endpoint with query and fragment",
			base:     "https://api.example.com/api",
			endpoint: "/v1/send?mode=test#section",
			want:     "https://api.example.com/api/v1/send?mode=test#section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, err := url.Parse(tt.base)
			if err != nil {
				t.Fatalf("failed to parse base URL: %v", err)
			}
			endpointURL, err := url.Parse(tt.endpoint)
			if err != nil {
				t.Fatalf("failed to parse endpoint URL: %v", err)
			}
			got := resolveSendURL(baseURL, endpointURL)
			if got.String() != tt.want {
				t.Errorf("resolveSendURL() = %v, want %v", got.String(), tt.want)
			}
		})
	}
}

func TestHTTPProvider_Send_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("Expected test-key, got %s", r.Header.Get("X-API-Key"))
		}

		// Send response
		resp := HTTPSendResponse{
			OK:        true,
			MessageID: "test-msg-123",
			Provider:  "test-provider",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := &HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		APIKey:       "test-key",
		APIKeyHeader: "X-API-Key",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers:      make(map[string]string),
	}

	httpProvider, err := NewHTTPProvider(config)
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	msg := NewMessage("+1234567890").
		WithBody("Test message").
		WithIdempotencyKey("idem-123")

	result, err := httpProvider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
	if result.MessageID != "test-msg-123" {
		t.Errorf("Send() MessageID = %v, want test-msg-123", result.MessageID)
	}
}

func TestHTTPProvider_Send_Failure(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := HTTPSendResponse{
			OK:           false,
			ErrorCode:    "rate_limited",
			ErrorMessage: "Too many requests",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	msg := NewMessage("+1234567890").WithBody("Test")

	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for failed response")
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}

	// Check error reason
	reason, ok := GetErrorReason(err)
	if !ok {
		t.Error("Error should be a ProviderError")
	}
	if reason != ReasonRateLimited {
		t.Errorf("Error reason = %v, want rate_limited", reason)
	}
}

func TestHTTPProvider_Send_ServerError(t *testing.T) {
	// Create test server that returns HTTP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	msg := NewMessage("+1234567890").WithBody("Test")

	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for server error")
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}
}

func TestHTTPProvider_Send_InvalidMessage(t *testing.T) {
	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "https://api.example.com",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
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

func TestNewHTTPProviderFromMap(t *testing.T) {
	configMap := map[string]string{
		"base_url":       "https://api.example.com",
		"send_endpoint":  "/send",
		"api_key":        "test-key",
		"api_key_header": "Authorization",
		"channel":        "sms",
		"name":           "custom",
	}

	provider, err := NewHTTPProviderFromMap(configMap)
	if err != nil {
		t.Fatalf("NewHTTPProviderFromMap() error = %v", err)
	}

	if provider.Channel() != ChannelSMS {
		t.Errorf("Channel() = %v, want sms", provider.Channel())
	}
	if provider.Name() != "custom" {
		t.Errorf("Name() = %v, want custom", provider.Name())
	}
}

func TestHTTPProviderFactory(t *testing.T) {
	configMap := map[string]string{
		"base_url": "https://api.example.com",
		"channel":  "email",
		"name":     "http-email",
	}

	provider, err := HTTPProviderFactory(configMap)
	if err != nil {
		t.Fatalf("HTTPProviderFactory() error = %v", err)
	}

	if provider.Name() != "http-email" {
		t.Errorf("Name() = %v, want http-email", provider.Name())
	}
}

func TestHTTPProvider_SetHTTPClient(t *testing.T) {
	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	customClient := &http.Client{}
	provider.SetHTTPClient(customClient)

	// Just verify it doesn't panic
}
