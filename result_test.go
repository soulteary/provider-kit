package provider

import "testing"

func TestNewSuccessResult(t *testing.T) {
	result := NewSuccessResult("smtp", ChannelEmail, "msg-123")

	if !result.OK {
		t.Error("NewSuccessResult() OK should be true")
	}
	if result.MessageID != "msg-123" {
		t.Errorf("NewSuccessResult() MessageID = %v, want msg-123", result.MessageID)
	}
	if result.Provider != "smtp" {
		t.Errorf("NewSuccessResult() Provider = %v, want smtp", result.Provider)
	}
	if result.Channel != ChannelEmail {
		t.Errorf("NewSuccessResult() Channel = %v, want email", result.Channel)
	}
	if result.Timestamp.IsZero() {
		t.Error("NewSuccessResult() Timestamp should not be zero")
	}
	if result.Metadata == nil {
		t.Error("NewSuccessResult() Metadata should not be nil")
	}
}

func TestNewFailureResult(t *testing.T) {
	err := ErrSendFailed("connection refused", nil)
	result := NewFailureResult("smtp", ChannelEmail, err)

	if result.OK {
		t.Error("NewFailureResult() OK should be false")
	}
	if result.Provider != "smtp" {
		t.Errorf("NewFailureResult() Provider = %v, want smtp", result.Provider)
	}
	if result.Channel != ChannelEmail {
		t.Errorf("NewFailureResult() Channel = %v, want email", result.Channel)
	}
	if result.Error != err {
		t.Error("NewFailureResult() Error should be the provided error")
	}
}

func TestSendResult_WithMetadata(t *testing.T) {
	result := NewSuccessResult("smtp", ChannelEmail, "msg-123").
		WithMetadata("key1", "value1").
		WithMetadata("key2", "value2")

	if result.Metadata["key1"] != "value1" {
		t.Errorf("WithMetadata() key1 = %v, want value1", result.Metadata["key1"])
	}
	if result.Metadata["key2"] != "value2" {
		t.Errorf("WithMetadata() key2 = %v, want value2", result.Metadata["key2"])
	}
}

func TestSendResult_GetError(t *testing.T) {
	tests := []struct {
		name    string
		result  *SendResult
		wantErr bool
	}{
		{
			name:    "success result",
			result:  NewSuccessResult("smtp", ChannelEmail, "msg-123"),
			wantErr: false,
		},
		{
			name:    "failure result with error",
			result:  NewFailureResult("smtp", ChannelEmail, ErrSendFailed("test", nil)),
			wantErr: true,
		},
		{
			name: "failure result without error",
			result: &SendResult{
				OK:       false,
				Provider: "smtp",
				Channel:  ChannelEmail,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.GetError()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetError() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
