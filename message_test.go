package provider

import (
	"testing"
)

func TestNewMessage(t *testing.T) {
	msg := NewMessage("test@example.com")

	if msg.To != "test@example.com" {
		t.Errorf("NewMessage() To = %v, want test@example.com", msg.To)
	}
	if msg.Params == nil {
		t.Error("NewMessage() Params should not be nil")
	}
	if msg.Metadata == nil {
		t.Error("NewMessage() Metadata should not be nil")
	}
}

func TestMessage_WithMethods(t *testing.T) {
	msg := NewMessage("test@example.com").
		WithSubject("Test Subject").
		WithBody("Test Body").
		WithTemplate("verification").
		WithCode("123456").
		WithLocale("en-US").
		WithIdempotencyKey("key-123")

	if msg.Subject != "Test Subject" {
		t.Errorf("WithSubject() = %v, want Test Subject", msg.Subject)
	}
	if msg.Body != "Test Body" {
		t.Errorf("WithBody() = %v, want Test Body", msg.Body)
	}
	if msg.Template != "verification" {
		t.Errorf("WithTemplate() = %v, want verification", msg.Template)
	}
	if msg.Code != "123456" {
		t.Errorf("WithCode() = %v, want 123456", msg.Code)
	}
	if msg.Locale != "en-US" {
		t.Errorf("WithLocale() = %v, want en-US", msg.Locale)
	}
	if msg.IdempotencyKey != "key-123" {
		t.Errorf("WithIdempotencyKey() = %v, want key-123", msg.IdempotencyKey)
	}
}

func TestMessage_WithParams(t *testing.T) {
	params := map[string]string{
		"code":   "123456",
		"expiry": "5",
	}

	msg := NewMessage("test@example.com").WithParams(params)

	if msg.Params["code"] != "123456" {
		t.Errorf("WithParams() code = %v, want 123456", msg.Params["code"])
	}
	if msg.Params["expiry"] != "5" {
		t.Errorf("WithParams() expiry = %v, want 5", msg.Params["expiry"])
	}
}

func TestMessage_WithParam(t *testing.T) {
	msg := NewMessage("test@example.com").
		WithParam("key1", "value1").
		WithParam("key2", "value2")

	if msg.Params["key1"] != "value1" {
		t.Errorf("WithParam() key1 = %v, want value1", msg.Params["key1"])
	}
	if msg.Params["key2"] != "value2" {
		t.Errorf("WithParam() key2 = %v, want value2", msg.Params["key2"])
	}
}

func TestMessage_WithMetadata(t *testing.T) {
	metadata := map[string]string{
		"trace_id": "abc123",
	}

	msg := NewMessage("test@example.com").WithMetadata(metadata)

	if msg.Metadata["trace_id"] != "abc123" {
		t.Errorf("WithMetadata() trace_id = %v, want abc123", msg.Metadata["trace_id"])
	}
}

func TestMessage_AddMetadata(t *testing.T) {
	msg := NewMessage("test@example.com").
		AddMetadata("key1", "value1").
		AddMetadata("key2", "value2")

	if msg.Metadata["key1"] != "value1" {
		t.Errorf("AddMetadata() key1 = %v, want value1", msg.Metadata["key1"])
	}
	if msg.Metadata["key2"] != "value2" {
		t.Errorf("AddMetadata() key2 = %v, want value2", msg.Metadata["key2"])
	}
}

func TestMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantErr bool
	}{
		{
			name:    "valid message",
			msg:     NewMessage("test@example.com"),
			wantErr: false,
		},
		{
			name:    "empty destination",
			msg:     NewMessage(""),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMessage_Clone(t *testing.T) {
	original := NewMessage("test@example.com").
		WithSubject("Test").
		WithBody("Body").
		WithParam("code", "123456").
		AddMetadata("trace_id", "abc")

	clone := original.Clone()

	// Check values are copied
	if clone.To != original.To {
		t.Errorf("Clone() To = %v, want %v", clone.To, original.To)
	}
	if clone.Subject != original.Subject {
		t.Errorf("Clone() Subject = %v, want %v", clone.Subject, original.Subject)
	}

	// Modify clone params and verify original unchanged
	clone.Params["code"] = "654321"
	if original.Params["code"] != "123456" {
		t.Error("Clone() should create deep copy of Params")
	}

	// Modify clone metadata and verify original unchanged
	clone.Metadata["trace_id"] = "xyz"
	if original.Metadata["trace_id"] != "abc" {
		t.Error("Clone() should create deep copy of Metadata")
	}
}

func TestMessage_Clone_Nil(t *testing.T) {
	var msg *Message
	clone := msg.Clone()
	if clone != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestMessage_String(t *testing.T) {
	msg := NewMessage("test@example.com").
		WithTemplate("verification").
		WithCode("123456").
		WithLocale("en-US")

	str := msg.String()

	// Should contain safe info
	if str == "" {
		t.Error("String() returned empty string")
	}

	// Should mask email
	if containsString(str, "test@example.com") {
		t.Error("String() should mask email address")
	}
}

func TestMaskDestination(t *testing.T) {
	tests := []struct {
		dest string
		want string
	}{
		{"test@example.com", "te***@example.com"},
		{"a@b.com", "***.com"},
		{"+1234567890", "***7890"},
		{"ab", "****"},
	}

	for _, tt := range tests {
		t.Run(tt.dest, func(t *testing.T) {
			got := maskDestination(tt.dest)
			if got != tt.want {
				t.Errorf("maskDestination(%v) = %v, want %v", tt.dest, got, tt.want)
			}
		})
	}
}
