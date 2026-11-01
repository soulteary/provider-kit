package provider

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSecurityAudit_HTTPConfigCannotDisableHTTPS(t *testing.T) {
	var plaintext, tlsCalls atomic.Int64
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { plaintext.Add(1); _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tlsCalls.Add(1)
		if r.Header.Get("X-Test") != "original" {
			t.Error("caller header mutation reached provider")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer secure.Close()
	cfg := &HTTPConfig{BaseURL: secure.URL, RequireHTTPS: true, APIKey: "test-key", Headers: map[string]string{"X-Test": "original"}, ChannelType: ChannelSMS}
	p, err := NewHTTPProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p.SetHTTPClient(secure.Client())
	cfg.BaseURL = insecure.URL
	cfg.Headers["X-Test"] = "mutated"
	result, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("test code: 123456"))
	if err != nil || !result.OK {
		t.Fatalf("send: result=%v err=%v", result, err)
	}
	if plaintext.Load() != 0 || tlsCalls.Load() != 1 {
		t.Fatalf("plaintext requests=%d TLS requests=%d", plaintext.Load(), tlsCalls.Load())
	}
}

func TestSecurityAudit_HTTPRejectsFalseSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		limit  int64
	}{
		{"error_status", 500, `{"ok":true}`, 1024},
		{"redirect_status_without_location", 300, `{"ok":true}`, 1024},
		{"truncated_success_prefix", 200, `{"ok":true}` + strings.Repeat("x", 32), 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p, err := NewHTTPProvider(&HTTPConfig{BaseURL: srv.URL, MaxResponseBytes: tc.limit, ChannelType: ChannelSMS})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("test"))
			if err == nil || result == nil || result.OK {
				t.Fatalf("untrusted response accepted: result=%v err=%v", result, err)
			}
		})
	}
}

func TestSecurityAudit_HTTPRejectsUnknownRedirectPolicy(t *testing.T) {
	if _, err := NewHTTPProvider(&HTTPConfig{BaseURL: "https://example.test", Redirect: "typo"}); err == nil {
		t.Fatal("unknown redirect policy accepted")
	}
}

func TestSecurityAudit_HTTPRejectsUntrustedCertificate(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer srv.Close()
	p, err := NewHTTPProvider(&HTTPConfig{BaseURL: srv.URL, RequireHTTPS: true, ChannelType: ChannelSMS})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("test"))
	if err == nil || result == nil || result.OK || calls.Load() != 0 {
		t.Fatalf("untrusted TLS endpoint received a message: result=%v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestSecurityAudit_SMTPRefusesSTARTTLSDowngrade(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	observed := make(chan []string, 1)
	go observeSTARTTLSRefusal(listener, observed)

	addr := listener.Addr().(*net.TCPAddr)
	p, err := NewSMTPProvider(&SMTPConfig{Host: "127.0.0.1", Port: addr.Port, From: "sender@example.com", Username: "test-user", Password: "test-password", UseStartTLS: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Send(context.Background(), NewMessage("recipient@example.com").WithBody("test code: 123456"))
	if err == nil || result == nil || result.OK {
		t.Fatalf("STARTTLS refusal accepted: result=%v err=%v", result, err)
	}
	select {
	case seen := <-observed:
		if len(seen) == 0 {
			t.Fatal("SMTP server never received handshake")
		}
		for _, line := range seen {
			for _, prefix := range []string{"AUTH", "MAIL", "RCPT", "DATA"} {
				if strings.HasPrefix(line, prefix) {
					t.Fatalf("plaintext downgrade sent %s", prefix)
				}
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP test server did not finish")
	}
}

func TestSecurityAudit_SMTPRejectsUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("SMTP connected beyond an untrusted TLS handshake")
	}))
	defer srv.Close()
	addr := srv.Listener.Addr().(*net.TCPAddr)
	p, err := NewSMTPProvider(&SMTPConfig{Host: "127.0.0.1", Port: addr.Port, From: "sender@example.com", UseTLS: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Send(context.Background(), NewMessage("recipient@example.com").WithBody("test code: 123456"))
	if err == nil || result == nil || result.OK {
		t.Fatalf("untrusted SMTP certificate accepted: result=%v err=%v", result, err)
	}
}

func TestSecurityAudit_HTTPBoundariesAndFailureReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int64
		status int
		body   string
		reason ErrorReason
	}{
		{"exact_limit", 11, 200, `{"ok":true}`, ""},
		{"max_limit", 1<<63 - 1, 200, `{"ok":true}`, ""},
		{"structured_failure", 1024, 429, `{"ok":false,"error_code":"rate_limited"}`, ReasonRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p, err := NewHTTPProvider(&HTTPConfig{BaseURL: srv.URL, MaxResponseBytes: tc.limit, ChannelType: ChannelSMS})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("test"))
			if tc.reason == "" {
				if err != nil || !result.OK {
					t.Fatalf("valid boundary rejected: %v %v", result, err)
				}
			} else if err == nil || result.OK || result.Error.Reason != tc.reason {
				t.Fatalf("failure reason changed: %v %v", result, err)
			}
		})
	}
}

func observeSTARTTLSRefusal(listener net.Listener, observed chan<- []string) {
	conn, err := listener.Accept()
	if err != nil {
		observed <- nil
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("220 audit SMTP\r\n"))
	var seen []string
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		seen = append(seen, line)
		reply := "550 refused\r\n"
		if strings.HasPrefix(line, "EHLO") {
			reply = "250 audit SMTP\r\n"
		}
		if strings.HasPrefix(line, "STARTTLS") {
			reply = "454 TLS unavailable\r\n"
		}
		if _, err = conn.Write([]byte(reply)); err != nil {
			break
		}
	}
	observed <- seen
}
