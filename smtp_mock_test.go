package provider

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
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

func mockSMTPPipe(t *testing.T, authSeen chan<- bool) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer func() { _ = server.Close() }()
		serveMockSMTP(server, authSeen, nil)
	}()
	return client
}

func mockSMTPTLSPipe(t *testing.T, startTLS bool) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	serverTLS := testServerTLSConfig(t)
	go func() {
		defer func() { _ = server.Close() }()
		if startTLS {
			serveMockSMTP(server, nil, serverTLS)
			return
		}
		tlsServer := tls.Server(server, serverTLS)
		if err := tlsServer.Handshake(); err != nil {
			return
		}
		serveMockSMTP(tlsServer, nil, nil)
	}()
	return client
}

func serveMockSMTP(conn net.Conn, authSeen chan<- bool, startTLSConfig *tls.Config) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeReply := func(reply string) bool {
		if _, err := writer.WriteString(reply); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !writeReply("220 mock SMTP\r\n") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			if startTLSConfig != nil {
				if !writeReply("250-mock\r\n250 STARTTLS\r\n") {
					return
				}
				continue
			}
			if !writeReply("250-mock\r\n250 AUTH PLAIN\r\n") {
				return
			}
		case strings.HasPrefix(upper, "HELO"):
			if !writeReply("250 mock\r\n") {
				return
			}
		case upper == "STARTTLS" && startTLSConfig != nil:
			if !writeReply("220 ready for TLS\r\n") {
				return
			}
			tlsServer := tls.Server(conn, startTLSConfig)
			if err := tlsServer.Handshake(); err != nil {
				return
			}
			conn = tlsServer
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)
			startTLSConfig = nil
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			if authSeen != nil {
				authSeen <- true
			}
			if !writeReply("235 authenticated\r\n") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
			if !writeReply("250 ok\r\n") {
				return
			}
		case upper == "DATA":
			if !writeReply("354 end with dot\r\n") {
				return
			}
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
			}
			if !writeReply("250 queued\r\n") {
				return
			}
		case upper == "QUIT":
			_ = writeReply("221 bye\r\n")
			if _, encrypted := conn.(*tls.Conn); !encrypted {
				return
			}
		default:
			if !writeReply("500 unsupported\r\n") {
				return
			}
		}
	}
}

func testServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() error = %v", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatalf("tls.X509KeyPair() error = %v", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
}

func replaceSMTPDial(t *testing.T, fn func(context.Context, string, string, time.Duration) (net.Conn, error)) {
	t.Helper()
	original := smtpDialContext
	smtpDialContext = fn
	t.Cleanup(func() { smtpDialContext = original })
}

func replaceSMTPNewClient(t *testing.T, fn func(net.Conn, string) (*smtp.Client, error)) {
	t.Helper()
	original := smtpNewClient
	smtpNewClient = fn
	t.Cleanup(func() { smtpNewClient = original })
}

func TestSMTPProvider_sendPlain_Success(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPPipe(t, nil), nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "localhost",
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
	authSeen := make(chan bool, 1)
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPPipe(t, authSeen), nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:        "localhost",
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

	select {
	case <-authSeen:
	case <-time.After(time.Second):
		t.Error("Auth should be attempted when username is provided")
	}
}

func TestSMTPProvider_sendPlain_Error(t *testing.T) {
	expectedErr := errors.New("connection refused")
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, expectedErr
	})

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

func TestSMTPProvider_sendWithTLS_DialError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, errors.New("TLS dial failed")
	})

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

func TestSMTPProvider_sendWithTLS_Success(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPTLSPipe(t, false), nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:          "localhost",
		Port:          465,
		From:          "sender@example.com",
		UseTLS:        true,
		SkipTLSVerify: true,
	})

	result, err := provider.Send(context.Background(), NewMessage("recipient@example.com").WithBody("Test"))
	if err != nil {
		t.Fatalf("Send() error = %v (cause: %v)", err, errors.Unwrap(err))
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestSMTPProvider_sendWithTLS_HandshakeError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		return &deadlineTestConn{Conn: client, closePeer: server}, nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "localhost", Port: 465, From: "sender@example.com", UseTLS: true,
	})
	result, err := provider.Send(context.Background(), NewMessage("recipient@example.com"))
	if err == nil || !strings.Contains(errors.Unwrap(err).Error(), "TLS handshake failed") {
		t.Fatalf("Send() error = %v (cause: %v), want TLS handshake failure", err, errors.Unwrap(err))
	}
	if result.OK {
		t.Error("Send() result.OK should be false")
	}
}

func TestSMTPProvider_sendWithTLS_ClientCreationError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPTLSPipe(t, false), nil
	})
	replaceSMTPNewClient(t, func(net.Conn, string) (*smtp.Client, error) {
		return nil, errors.New("client creation failed")
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "localhost", Port: 465, From: "sender@example.com", UseTLS: true, SkipTLSVerify: true,
	})
	_, err := provider.Send(context.Background(), NewMessage("recipient@example.com"))
	if err == nil || !strings.Contains(errors.Unwrap(err).Error(), "SMTP client creation failed") {
		t.Fatalf("Send() error = %v (cause: %v), want client creation failure", err, errors.Unwrap(err))
	}
}

func TestSMTPProvider_sendWithStartTLS_DialError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, errors.New("dial failed")
	})

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

func TestSMTPProvider_sendWithStartTLS_Success(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPTLSPipe(t, true), nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host:          "localhost",
		Port:          587,
		From:          "sender@example.com",
		UseStartTLS:   true,
		SkipTLSVerify: true,
	})

	result, err := provider.Send(context.Background(), NewMessage("recipient@example.com").WithBody("Test"))
	if err != nil {
		t.Fatalf("Send() error = %v (cause: %v)", err, errors.Unwrap(err))
	}
	if !result.OK {
		t.Error("Send() result.OK should be true")
	}
}

func TestSMTPProvider_sendWithStartTLS_ClientCreationError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPPipe(t, nil), nil
	})
	replaceSMTPNewClient(t, func(net.Conn, string) (*smtp.Client, error) {
		return nil, errors.New("client creation failed")
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "localhost", Port: 587, From: "sender@example.com", UseStartTLS: true,
	})
	_, err := provider.Send(context.Background(), NewMessage("recipient@example.com"))
	if err == nil || !strings.Contains(errors.Unwrap(err).Error(), "SMTP client creation failed") {
		t.Fatalf("Send() error = %v (cause: %v), want client creation failure", err, errors.Unwrap(err))
	}
}

func TestSMTPProvider_sendWithStartTLS_UpgradeError(t *testing.T) {
	replaceSMTPDial(t, func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return mockSMTPPipe(t, nil), nil
	})

	provider, _ := NewSMTPProvider(&SMTPConfig{
		Host: "localhost", Port: 587, From: "sender@example.com", UseStartTLS: true,
	})
	_, err := provider.Send(context.Background(), NewMessage("recipient@example.com"))
	if err == nil || !strings.Contains(errors.Unwrap(err).Error(), "STARTTLS failed") {
		t.Fatalf("Send() error = %v (cause: %v), want STARTTLS failure", err, errors.Unwrap(err))
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
