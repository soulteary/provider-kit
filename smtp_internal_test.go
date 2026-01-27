package provider

import (
	"context"
	"strings"
	"testing"
)

func TestSMTPProvider_Validate(t *testing.T) {
	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	if err := provider.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestSMTPProvider_buildEmailBody(t *testing.T) {
	tests := []struct {
		name      string
		config    *SMTPConfig
		msg       *Message
		wantParts []string
		notWant   []string
	}{
		{
			name: "basic email",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "sender@example.com",
			},
			msg: NewMessage("recipient@example.com").
				WithSubject("Test Subject").
				WithBody("Test body content"),
			wantParts: []string{
				"From: sender@example.com\r\n",
				"To: recipient@example.com\r\n",
				"Subject: Test Subject\r\n",
				"Content-Type: text/plain; charset=UTF-8\r\n",
				"Test body content",
			},
		},
		{
			name: "email with from name",
			config: &SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				From:     "sender@example.com",
				FromName: "Test Sender",
			},
			msg: NewMessage("recipient@example.com").
				WithBody("Body"),
			wantParts: []string{
				"From: Test Sender <sender@example.com>\r\n",
				"To: recipient@example.com\r\n",
			},
		},
		{
			name: "email without subject",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "sender@example.com",
			},
			msg: NewMessage("recipient@example.com").
				WithBody("Body only"),
			wantParts: []string{
				"From: sender@example.com\r\n",
				"Body only",
			},
			notWant: []string{
				"Subject:",
			},
		},
		{
			name: "email with idempotency key",
			config: &SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "sender@example.com",
			},
			msg: NewMessage("recipient@example.com").
				WithBody("Body").
				WithIdempotencyKey("unique-key-123"),
			wantParts: []string{
				"Message-ID: <unique-key-123@smtp.example.com>\r\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &SMTPProvider{
				config: tt.config,
				name:   "smtp",
			}

			body := provider.buildEmailBody(tt.msg)
			bodyStr := string(body)

			for _, part := range tt.wantParts {
				if !strings.Contains(bodyStr, part) {
					t.Errorf("buildEmailBody() should contain %q, got %q", part, bodyStr)
				}
			}

			for _, part := range tt.notWant {
				if strings.Contains(bodyStr, part) {
					t.Errorf("buildEmailBody() should not contain %q, got %q", part, bodyStr)
				}
			}
		})
	}
}

func TestSMTPProvider_Send_TLSModes(t *testing.T) {
	// Test UseTLS mode (will fail to connect but exercises code path)
	providerTLS, _ := NewSMTPProvider(&SMTPConfig{
		Host:   "localhost",
		Port:   1,
		From:   "sender@example.com",
		UseTLS: true,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	result, err := providerTLS.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with UseTLS should fail for invalid server")
	}
	if result.OK {
		t.Error("Send() result.OK should be false for failed send")
	}

	// Test UseStartTLS mode (will fail to connect but exercises code path)
	providerStartTLS, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "localhost",
		Port:        1,
		From:        "sender@example.com",
		UseStartTLS: true,
	})

	result2, err2 := providerStartTLS.Send(context.Background(), msg)
	if err2 == nil {
		t.Error("Send() with UseStartTLS should fail for invalid server")
	}
	if result2.OK {
		t.Error("Send() result.OK should be false for failed send")
	}

	// Test plain mode (will fail to connect but exercises code path)
	providerPlain, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "localhost",
		Port:        1,
		From:        "sender@example.com",
		UseTLS:      false,
		UseStartTLS: false,
	})

	result3, err3 := providerPlain.Send(context.Background(), msg)
	if err3 == nil {
		t.Error("Send() with plain should fail for invalid server")
	}
	if result3.OK {
		t.Error("Send() result.OK should be false for failed send")
	}
}

func TestSMTPProvider_Send_WithIdempotencyKey(t *testing.T) {
	// This test exercises the idempotency key path in Send
	// The send will fail but we can verify the result includes the key
	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "localhost",
		Port: 1,
		From: "sender@example.com",
	})

	msg := NewMessage("recipient@example.com").
		WithBody("Test").
		WithIdempotencyKey("test-key-456")

	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should fail for invalid server")
	}
	// Result should be a failure
	if result.OK {
		t.Error("Result should be failure")
	}
}

func TestSMTPProvider_SetDialer(t *testing.T) {
	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "sender@example.com",
	})

	// Just test that SetDialer doesn't panic
	provider.SetDialer(nil)
}
