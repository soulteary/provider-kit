package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProvider_Validate_Method(t *testing.T) {
	provider, err := NewHTTPProvider(&HTTPConfig{
		BaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider() error = %v", err)
	}

	if err := provider.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestHTTPProvider_mapErrorCode_AllCodes(t *testing.T) {
	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "https://api.example.com",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
	})

	tests := []struct {
		code       string
		message    string
		wantReason ErrorReason
	}{
		{"rate_limited", "too many requests", ReasonRateLimited},
		{"invalid_destination", "bad phone", ReasonInvalidDestination},
		{"timeout", "request timeout", ReasonTimeout},
		{"unauthorized", "bad api key", ReasonUnauthorized},
		{"provider_down", "service unavailable", ReasonProviderDown},
		{"idempotency_conflict", "duplicate request", ReasonIdempotencyConflict},
		{"unknown_code", "some error", ReasonSendFailed},
		{"", "no code", ReasonSendFailed},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			err := provider.mapErrorCode(tt.code, tt.message)
			if err.Reason != tt.wantReason {
				t.Errorf("mapErrorCode(%q) Reason = %v, want %v", tt.code, err.Reason, tt.wantReason)
			}
			if err.Message != tt.message {
				t.Errorf("mapErrorCode(%q) Message = %v, want %v", tt.code, err.Message, tt.message)
			}
		})
	}
}

func TestHTTPProvider_Send_WithoutIdempotencyKey(t *testing.T) {
	// Test that a UUID is generated when no idempotency key is provided
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("Idempotency-Key")
		resp := HTTPSendResponse{OK: true, MessageID: "msg-123"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers:      make(map[string]string),
	})

	msg := NewMessage("+1234567890").WithBody("Test")
	_, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedKey == "" {
		t.Error("Idempotency-Key should be auto-generated")
	}
}

func TestHTTPProvider_Send_WithCustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		resp := HTTPSendResponse{OK: true, MessageID: "msg-123"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-Trace-ID":      "trace-123",
		},
	})

	msg := NewMessage("+1234567890").WithBody("Test")
	_, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Error("Custom header not sent")
	}
	if receivedHeaders.Get("X-Trace-ID") != "trace-123" {
		t.Error("Trace ID header not sent")
	}
}

func TestHTTPProvider_Send_ParseResponseError(t *testing.T) {
	// Server returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers:      make(map[string]string),
	})

	msg := NewMessage("+1234567890").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for invalid JSON response")
	}
	if result.OK {
		t.Error("Result should be failure")
	}
}

func TestHTTPProvider_Send_WithUpstreamProvider(t *testing.T) {
	// Server returns upstream provider info
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := HTTPSendResponse{
			OK:        true,
			MessageID: "msg-123",
			Provider:  "upstream-provider",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers:      make(map[string]string),
	})

	msg := NewMessage("+1234567890").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if result.Metadata["upstream_provider"] != "upstream-provider" {
		t.Error("Upstream provider should be in metadata")
	}
}

func TestHTTPProvider_Send_ConnectionError(t *testing.T) {
	// Use an invalid URL that will fail to connect
	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "http://localhost:1", // Port 1 should fail
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Timeout:      1 * time.Second,
		Headers:      make(map[string]string),
	})

	msg := NewMessage("+1234567890").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for connection failure")
	}
	if result.OK {
		t.Error("Result should be failure")
	}

	// Check error reason
	reason, ok := GetErrorReason(err)
	if !ok {
		t.Error("Error should be a ProviderError")
	}
	if reason != ReasonProviderDown {
		t.Errorf("Error reason = %v, want provider_down", reason)
	}
}

func TestHTTPProvider_Send_AllTemplateFields(t *testing.T) {
	var receivedBody HTTPSendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		resp := HTTPSendResponse{OK: true, MessageID: "msg-123"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, _ := NewHTTPProvider(&HTTPConfig{
		BaseURL:      server.URL,
		SendEndpoint: "/v1/send",
		ChannelType:  ChannelEmail,
		ProviderName: "test",
		Timeout:      30 * time.Second,
		Headers:      make(map[string]string),
	})

	msg := NewMessage("test@example.com").
		WithSubject("Test Subject").
		WithBody("Test Body").
		WithTemplate("verification").
		WithParams(map[string]string{"code": "123456", "expiry": "5"}).
		WithLocale("zh-CN").
		WithIdempotencyKey("idem-key-789")

	_, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Verify all fields were sent
	if receivedBody.To != "test@example.com" {
		t.Errorf("To = %v, want test@example.com", receivedBody.To)
	}
	if receivedBody.Subject != "Test Subject" {
		t.Errorf("Subject = %v, want Test Subject", receivedBody.Subject)
	}
	if receivedBody.Body != "Test Body" {
		t.Errorf("Body = %v, want Test Body", receivedBody.Body)
	}
	if receivedBody.Template != "verification" {
		t.Errorf("Template = %v, want verification", receivedBody.Template)
	}
	if receivedBody.Locale != "zh-CN" {
		t.Errorf("Locale = %v, want zh-CN", receivedBody.Locale)
	}
	if receivedBody.IdempotencyKey != "idem-key-789" {
		t.Errorf("IdempotencyKey = %v, want idem-key-789", receivedBody.IdempotencyKey)
	}
	if receivedBody.Params["code"] != "123456" {
		t.Errorf("Params[code] = %v, want 123456", receivedBody.Params["code"])
	}
}
