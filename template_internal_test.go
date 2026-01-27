package provider

import (
	"strings"
	"testing"
)

func TestFormatVerificationSMS_AllLocales(t *testing.T) {
	tests := []struct {
		locale      string
		wantContain string
	}{
		{"en-US", "verification code"},
		{"en", "verification code"},
		{"zh-CN", "验证码"},
		{"zh-TW", "验证码"},
		{"zh", "验证码"},
		{"ja-JP", "認証コード"},
		{"ja", "認証コード"},
		{"ko-KR", "인증 코드"},
		{"ko", "인증 코드"},
		{"de-DE", "Verifizierungscode"},
		{"de", "Verifizierungscode"},
		{"fr-FR", "code"},
		{"fr", "code"},
		{"es-ES", "código"},
		{"es", "código"},
		{"it-IT", "verification code"}, // Falls back to English
		{"pt-BR", "verification code"}, // Falls back to English
		{"", "verification code"},      // Empty locale falls back
	}

	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			result := FormatVerificationSMS("123456", tt.locale)
			lowerResult := strings.ToLower(result)
			lowerWant := strings.ToLower(tt.wantContain)

			if !strings.Contains(lowerResult, lowerWant) && !strings.Contains(result, tt.wantContain) {
				t.Errorf("FormatVerificationSMS(%q) = %q, should contain %q", tt.locale, result, tt.wantContain)
			}
		})
	}
}

func TestBuildVerificationMessage_AllChannels(t *testing.T) {
	tests := []struct {
		channel Channel
		hasSubj bool
		hasBody bool
	}{
		{ChannelEmail, true, true},
		{ChannelSMS, false, true},
		{ChannelHTTP, false, false}, // Unknown channel
		{Channel("custom"), false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.channel), func(t *testing.T) {
			msg := BuildVerificationMessage("test@example.com", "123456", "en-US", tt.channel)

			if tt.hasSubj && msg.Subject == "" {
				t.Error("Subject should not be empty")
			}
			if !tt.hasSubj && msg.Subject != "" {
				t.Errorf("Subject should be empty for channel %s", tt.channel)
			}
			if tt.hasBody && msg.Body == "" {
				t.Error("Body should not be empty")
			}
		})
	}
}
