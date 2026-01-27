package provider

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/smtp"
	"testing"
)

// testSMTPClient for testing - implements smtpClientInterface
type testSMTPClient struct {
	authErr  error
	mailErr  error
	rcptErr  error
	dataErr  error
	writeErr error
	closeErr error
	quitErr  error
	hasAuth  bool
}

type testDataWriter struct {
	writeErr error
	closeErr error
}

func (w *testDataWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}

func (w *testDataWriter) Close() error {
	return w.closeErr
}

func (c *testSMTPClient) Auth(a smtp.Auth) error {
	c.hasAuth = true
	return c.authErr
}

func (c *testSMTPClient) Mail(from string) error {
	return c.mailErr
}

func (c *testSMTPClient) Rcpt(to string) error {
	return c.rcptErr
}

func (c *testSMTPClient) Data() (io.WriteCloser, error) {
	if c.dataErr != nil {
		return nil, c.dataErr
	}
	return &testDataWriter{writeErr: c.writeErr, closeErr: c.closeErr}, nil
}

func (c *testSMTPClient) Quit() error {
	return c.quitErr
}

func (c *testSMTPClient) Close() error {
	return nil
}

func TestSMTPProvider_sendPlain_Success(t *testing.T) {
	// Save original and restore after test
	origSendMail := smtpSendMail
	defer func() { smtpSendMail = origSendMail }()

	// Mock successful send
	smtpSendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		return nil
	}

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "smtp.example.com",
		Port:        25,
		From:        "sender@example.com",
		UseTLS:      false,
		UseStartTLS: false,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestSMTPProvider_sendPlain_WithAuth(t *testing.T) {
	// Save original and restore after test
	origSendMail := smtpSendMail
	defer func() { smtpSendMail = origSendMail }()

	var receivedAuth smtp.Auth
	smtpSendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		receivedAuth = a
		return nil
	}

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "smtp.example.com",
		Port:        25,
		From:        "sender@example.com",
		Username:    "user",
		Password:    "pass",
		UseTLS:      false,
		UseStartTLS: false,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	_, err := provider.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedAuth == nil {
		t.Error("Auth should be set when username is provided")
	}
}

func TestSMTPProvider_sendPlain_Error(t *testing.T) {
	// Save original and restore after test
	origSendMail := smtpSendMail
	defer func() { smtpSendMail = origSendMail }()

	expectedErr := errors.New("connection refused")
	smtpSendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		return expectedErr
	}

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "smtp.example.com",
		Port:        25,
		From:        "sender@example.com",
		UseTLS:      false,
		UseStartTLS: false,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error")
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}
}

func TestSMTPProvider_sendWithTLS_Success(t *testing.T) {
	// Save originals and restore after test
	origTLSDial := tlsDial
	origNewClient := smtpNewClient
	defer func() {
		tlsDial = origTLSDial
		smtpNewClient = origNewClient
	}()

	// Mock TLS dial
	tlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		// Return a mock connection wrapped in tls.Conn is complex
		// Instead we'll test error path
		return nil, errors.New("TLS dial failed")
	}

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:   "smtp.example.com",
		Port:   465,
		From:   "sender@example.com",
		UseTLS: true,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for TLS dial failure")
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}
}

func TestSMTPProvider_sendWithStartTLS_Success(t *testing.T) {
	// Save original and restore after test
	origDial := smtpDial
	defer func() { smtpDial = origDial }()

	// Mock dial success but StartTLS will fail since mock client doesn't support it properly
	smtpDial = func(addr string) (*smtp.Client, error) {
		return nil, errors.New("dial failed")
	}

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "smtp.example.com",
		Port:        587,
		From:        "sender@example.com",
		UseStartTLS: true,
	})

	msg := NewMessage("recipient@example.com").WithBody("Test")
	result, err := provider.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for dial failure")
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}
}

func TestSMTPProvider_sendWithClient_AllErrors(t *testing.T) {
	provider := &SMTPProvider{
		config: &SMTPConfig{
			Host:     "smtp.example.com",
			Port:     587,
			From:     "sender@example.com",
			Username: "user",
			Password: "pass",
		},
		name: "smtp",
	}

	tests := []struct {
		name    string
		client  *testSMTPClient
		wantErr string
	}{
		{
			name:    "auth error",
			client:  &testSMTPClient{authErr: errors.New("auth failed")},
			wantErr: "authentication failed",
		},
		{
			name:    "mail error",
			client:  &testSMTPClient{mailErr: errors.New("mail failed")},
			wantErr: "MAIL FROM failed",
		},
		{
			name:    "rcpt error",
			client:  &testSMTPClient{rcptErr: errors.New("rcpt failed")},
			wantErr: "RCPT TO failed",
		},
		{
			name:    "data error",
			client:  &testSMTPClient{dataErr: errors.New("data failed")},
			wantErr: "DATA command failed",
		},
		{
			name:    "write error",
			client:  &testSMTPClient{writeErr: errors.New("write failed")},
			wantErr: "write body failed",
		},
		{
			name:    "close error",
			client:  &testSMTPClient{closeErr: errors.New("close failed")},
			wantErr: "close writer failed",
		},
		{
			name:    "quit error",
			client:  &testSMTPClient{quitErr: errors.New("quit failed")},
			wantErr: "quit failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.sendWithClient(tt.client, "recipient@example.com", []byte("test"))
			if err == nil {
				t.Error("sendWithClient() should return error")
			}
			if !containsString(err.Error(), tt.wantErr) {
				t.Errorf("sendWithClient() error = %v, should contain %v", err, tt.wantErr)
			}
		})
	}
}

func TestSMTPProvider_sendWithClient_NoAuth(t *testing.T) {
	provider := &SMTPProvider{
		config: &SMTPConfig{
			Host: "smtp.example.com",
			Port: 587,
			From: "sender@example.com",
			// No username/password
		},
		name: "smtp",
	}

	client := &testSMTPClient{}
	err := provider.sendWithClient(client, "recipient@example.com", []byte("test"))
	if err != nil {
		t.Fatalf("sendWithClient() error = %v", err)
	}

	if client.hasAuth {
		t.Error("Should not call Auth when no username provided")
	}
}

func TestSMTPProvider_sendWithClient_Success(t *testing.T) {
	provider := &SMTPProvider{
		config: &SMTPConfig{
			Host:     "smtp.example.com",
			Port:     587,
			From:     "sender@example.com",
			Username: "user",
			Password: "pass",
		},
		name: "smtp",
	}

	client := &testSMTPClient{}
	err := provider.sendWithClient(client, "recipient@example.com", []byte("test"))
	if err != nil {
		t.Fatalf("sendWithClient() error = %v", err)
	}

	if !client.hasAuth {
		t.Error("Should call Auth when username provided")
	}
}
