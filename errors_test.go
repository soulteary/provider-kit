package provider

import (
	"errors"
	"testing"
)

func TestErrorReason_String(t *testing.T) {
	tests := []struct {
		reason ErrorReason
		want   string
	}{
		{ReasonSendFailed, "send_failed"},
		{ReasonProviderDown, "provider_down"},
		{ReasonInvalidConfig, "invalid_config"},
		{ReasonRateLimited, "rate_limited"},
		{ReasonTimeout, "timeout"},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.String(); got != tt.want {
				t.Errorf("ErrorReason.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ProviderError
		contains string
	}{
		{
			name: "with provider",
			err: &ProviderError{
				Reason:       ReasonSendFailed,
				Message:      "connection refused",
				ProviderName: "smtp",
				Channel:      ChannelEmail,
			},
			contains: "smtp",
		},
		{
			name: "without provider",
			err: &ProviderError{
				Reason:  ReasonSendFailed,
				Message: "connection refused",
				Channel: ChannelEmail,
			},
			contains: "email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got == "" {
				t.Error("Error() returned empty string")
			}
			if !containsString(got, tt.contains) {
				t.Errorf("Error() = %v, should contain %v", got, tt.contains)
			}
		})
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &ProviderError{
		Reason: ReasonSendFailed,
		Err:    underlying,
	}

	if err.Unwrap() != underlying {
		t.Error("Unwrap() did not return underlying error")
	}
}

func TestProviderError_WithProvider(t *testing.T) {
	err := NewProviderError(ReasonSendFailed, "test")
	err = err.WithProvider("smtp", ChannelEmail)

	if err.ProviderName != "smtp" {
		t.Errorf("WithProvider() ProviderName = %v, want smtp", err.ProviderName)
	}
	if err.Channel != ChannelEmail {
		t.Errorf("WithProvider() Channel = %v, want email", err.Channel)
	}
}

func TestProviderError_WithError(t *testing.T) {
	underlying := errors.New("test")
	err := NewProviderError(ReasonSendFailed, "message").WithError(underlying)

	if err.Err != underlying {
		t.Error("WithError() did not set underlying error")
	}
}

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name   string
		err    *ProviderError
		reason ErrorReason
	}{
		{
			name:   "ErrSendFailed",
			err:    ErrSendFailed("test", nil),
			reason: ReasonSendFailed,
		},
		{
			name:   "ErrProviderDown",
			err:    ErrProviderDown("test", nil),
			reason: ReasonProviderDown,
		},
		{
			name:   "ErrInvalidConfig",
			err:    ErrInvalidConfig("test"),
			reason: ReasonInvalidConfig,
		},
		{
			name:   "ErrRateLimited",
			err:    ErrRateLimited("test"),
			reason: ReasonRateLimited,
		},
		{
			name:   "ErrInvalidDestination",
			err:    ErrInvalidDestination("test"),
			reason: ReasonInvalidDestination,
		},
		{
			name:   "ErrTimeout",
			err:    ErrTimeout("test", nil),
			reason: ReasonTimeout,
		},
		{
			name:   "ErrUnauthorized",
			err:    ErrUnauthorized("test"),
			reason: ReasonUnauthorized,
		},
		{
			name:   "ErrNotRegistered",
			err:    ErrNotRegistered(ChannelEmail),
			reason: ReasonNotRegistered,
		},
		{
			name:   "ErrValidationFailed",
			err:    ErrValidationFailed("test"),
			reason: ReasonValidationFailed,
		},
		{
			name:   "ErrIdempotencyConflict",
			err:    ErrIdempotencyConflict("test"),
			reason: ReasonIdempotencyConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Reason != tt.reason {
				t.Errorf("%s Reason = %v, want %v", tt.name, tt.err.Reason, tt.reason)
			}
		})
	}
}

func TestIsProviderError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "is provider error",
			err:  ErrSendFailed("test", nil),
			want: true,
		},
		{
			name: "not provider error",
			err:  errors.New("standard error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProviderError(tt.err); got != tt.want {
				t.Errorf("IsProviderError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetErrorReason(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantReason ErrorReason
		wantOk     bool
	}{
		{
			name:       "provider error",
			err:        ErrSendFailed("test", nil),
			wantReason: ReasonSendFailed,
			wantOk:     true,
		},
		{
			name:       "standard error",
			err:        errors.New("test"),
			wantReason: "",
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := GetErrorReason(tt.err)
			if ok != tt.wantOk {
				t.Errorf("GetErrorReason() ok = %v, want %v", ok, tt.wantOk)
			}
			if reason != tt.wantReason {
				t.Errorf("GetErrorReason() reason = %v, want %v", reason, tt.wantReason)
			}
		})
	}
}

func TestNormalizeError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		channel      Channel
		providerName string
		wantReason   ErrorReason
	}{
		{
			name:         "nil error",
			err:          nil,
			channel:      ChannelEmail,
			providerName: "smtp",
			wantReason:   "",
		},
		{
			name:         "provider error",
			err:          ErrRateLimited("rate limited"),
			channel:      ChannelEmail,
			providerName: "smtp",
			wantReason:   ReasonRateLimited,
		},
		{
			name:         "standard error",
			err:          errors.New("connection refused"),
			channel:      ChannelSMS,
			providerName: "twilio",
			wantReason:   ReasonSendFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeError(tt.err, tt.channel, tt.providerName)
			if tt.err == nil {
				if result != nil {
					t.Error("NormalizeError(nil) should return nil")
				}
				return
			}
			if result.Reason != tt.wantReason {
				t.Errorf("NormalizeError() Reason = %v, want %v", result.Reason, tt.wantReason)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
