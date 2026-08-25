package provider

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type deadlineTestConn struct {
	net.Conn
	deadline    time.Time
	deadlineErr error
	closed      bool
	closePeer   net.Conn
}

func (c *deadlineTestConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	if c.deadlineErr != nil {
		return c.deadlineErr
	}
	if err := c.Conn.SetDeadline(deadline); err != nil {
		return err
	}
	if c.closePeer != nil {
		_ = c.closePeer.Close()
	}
	return nil
}

func (c *deadlineTestConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

func TestSMTPConfig_ValidateRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SMTPConfig)
	}{
		{name: "host control character", mutate: func(c *SMTPConfig) { c.Host = "smtp.example.com\r\nX-Test: value" }},
		{name: "host whitespace", mutate: func(c *SMTPConfig) { c.Host = "smtp example.com" }},
		{name: "invalid from address", mutate: func(c *SMTPConfig) { c.From = "not-an-email" }},
		{name: "from header injection", mutate: func(c *SMTPConfig) { c.From = "sender@example.com\r\nBcc: victim@example.com" }},
		{name: "from name injection", mutate: func(c *SMTPConfig) { c.FromName = "Sender\r\nBcc: victim@example.com" }},
		{name: "negative timeout", mutate: func(c *SMTPConfig) { c.Timeout = -time.Second }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SMTPConfig{
				Host:    "smtp.example.com",
				Port:    587,
				From:    "sender@example.com",
				Timeout: time.Second,
			}
			tt.mutate(config)
			if err := config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want invalid configuration error")
			}
		})
	}
}

func TestSMTPProvider_SendRejectsUnsafeHeaders(t *testing.T) {
	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "sender@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	tests := []struct {
		name   string
		msg    *Message
		reason ErrorReason
	}{
		{name: "nil message", msg: nil, reason: ReasonValidationFailed},
		{name: "invalid recipient", msg: NewMessage("not-an-email"), reason: ReasonInvalidDestination},
		{name: "recipient injection", msg: NewMessage("victim@example.com\r\nBcc: other@example.com"), reason: ReasonInvalidDestination},
		{name: "subject injection", msg: NewMessage("recipient@example.com").WithSubject("Hello\r\nBcc: other@example.com"), reason: ReasonValidationFailed},
		{name: "subject invalid UTF-8", msg: NewMessage("recipient@example.com").WithSubject(string([]byte{0xff})), reason: ReasonValidationFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, sendErr := provider.Send(context.Background(), tt.msg)
			if sendErr == nil {
				t.Fatal("Send() error = nil, want validation error")
			}
			if result == nil || result.OK || result.Error == nil {
				t.Fatalf("Send() result = %#v, want failure result", result)
			}
			if result.Error.Reason != tt.reason {
				t.Errorf("Send() reason = %q, want %q", result.Error.Reason, tt.reason)
			}
		})
	}
}

func TestSMTPProvider_DialUsesEarlierContextDeadline(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	connection := &deadlineTestConn{Conn: client}
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return connection, nil
	})

	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com", Port: 25, From: "sender@example.com", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	contextDeadline, _ := ctx.Deadline()
	_, cleanup, err := provider.dial(ctx, "smtp.example.com:25")
	if err != nil {
		t.Fatalf("dial() error = %v", err)
	}
	cleanup()
	if delta := connection.deadline.Sub(contextDeadline); delta < -time.Millisecond || delta > time.Millisecond {
		t.Errorf("connection deadline = %v, want context deadline %v", connection.deadline, contextDeadline)
	}
}

func TestSMTPProvider_DialClosesConnectionWhenSettingDeadlineFails(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	expectedErr := errors.New("deadline unsupported")
	connection := &deadlineTestConn{Conn: client, deadlineErr: expectedErr}
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return connection, nil
	})

	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "smtp.example.com", Port: 25, From: "sender@example.com", Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}
	_, cleanup, err := provider.dial(context.Background(), "smtp.example.com:25")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("dial() error = %v, want %v", err, expectedErr)
	}
	if cleanup != nil {
		t.Fatal("dial() cleanup should be nil after deadline failure")
	}
	if !connection.closed {
		t.Fatal("dial() should close connection after deadline failure")
	}
}

func TestMessageIDDomainFallback(t *testing.T) {
	if got := messageIDDomain("invalid-address"); got != "localhost" {
		t.Errorf("messageIDDomain() = %q, want localhost", got)
	}
}

func TestSMTPProvider_SendHonorsConfiguredTimeout(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	})

	provider, err := NewSMTPProvider(&SMTPConfig{
		Host:    "smtp.example.com",
		Port:    25,
		From:    "sender@example.com",
		Timeout: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	started := time.Now()
	result, sendErr := provider.Send(context.Background(), NewMessage("recipient@example.com"))
	if sendErr == nil {
		t.Fatal("Send() error = nil, want timeout")
	}
	if time.Since(started) > time.Second {
		t.Fatal("Send() did not honor configured timeout")
	}
	if result == nil || result.Error == nil || result.Error.Reason != ReasonTimeout {
		t.Fatalf("Send() result = %#v, want timeout failure", result)
	}
}

func TestSMTPProvider_SendHonorsContextCancellation(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	})

	provider, err := NewSMTPProvider(&SMTPConfig{
		Host:    "smtp.example.com",
		Port:    25,
		From:    "sender@example.com",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)
	result, sendErr := provider.Send(ctx, NewMessage("recipient@example.com"))
	if !errors.Is(sendErr, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", sendErr)
	}
	if result == nil || result.Error == nil || !strings.Contains(result.Error.Message, "canceled") {
		t.Fatalf("Send() result = %#v, want canceled failure", result)
	}
}

func TestSMTPProvider_DefaultsZeroTimeoutWithoutMutatingInput(t *testing.T) {
	config := &SMTPConfig{Host: "smtp.example.com", Port: 25, From: "sender@example.com"}
	provider, err := NewSMTPProvider(config)
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}
	if provider.config.Timeout != 30*time.Second {
		t.Errorf("provider timeout = %v, want 30s", provider.config.Timeout)
	}
	if config.Timeout != 0 {
		t.Errorf("input config timeout = %v, want unchanged zero value", config.Timeout)
	}
}

func TestSMTPProvider_PreservesIdempotencyResultWithoutUsingItAsHeader(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPPipe(t, nil), nil
	})

	provider, err := NewSMTPProvider(&SMTPConfig{
		Host: "localhost",
		Port: 25,
		From: "sender@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPProvider() error = %v", err)
	}

	const idempotencyKey = "caller-key-123"
	result, sendErr := provider.Send(
		context.Background(),
		NewMessage("recipient@example.com").WithIdempotencyKey(idempotencyKey),
	)
	if sendErr != nil {
		t.Fatalf("Send() error = %v", sendErr)
	}
	if result.MessageID != idempotencyKey {
		t.Errorf("Send() message ID = %q, want %q", result.MessageID, idempotencyKey)
	}
}
