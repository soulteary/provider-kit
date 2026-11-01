package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPConfigRejectsMalformedOrigins(t *testing.T) {
	for _, baseURL := range []string{"https://example.test/%zz", "file:///tmp/send", "https:///send", "example.test"} {
		t.Run(baseURL, func(t *testing.T) {
			p, err := NewHTTPProvider(&HTTPConfig{BaseURL: baseURL})
			if p != nil || err == nil {
				t.Fatalf("NewHTTPProvider = %v, %v", p, err)
			}
			if reason, ok := GetErrorReason(err); !ok || reason != ReasonInvalidConfig {
				t.Fatalf("invalid URL reason = %v, %v", reason, ok)
			}
		})
	}
}

func TestHTTPRejectsUnsafeEndpointsBeforeTransport(t *testing.T) {
	for _, endpoint := range []string{"/%zz", "https://other.test/send", "//other.test/send", "http://example.test/send"} {
		t.Run(endpoint, func(t *testing.T) {
			p, err := NewHTTPProvider(&HTTPConfig{BaseURL: "https://example.test", SendEndpoint: endpoint, ProviderName: "test", ChannelType: ChannelSMS})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			p.SetHTTPClient(&http.Client{Transport: boundaryTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected request")
			})})
			result, err := p.Send(context.Background(), NewMessage("+15551234567").WithCode("123456"))
			if err == nil || result.OK || result.Provider != "test" || result.Channel != ChannelSMS {
				t.Fatalf("Send = %+v, %v", result, err)
			}
			if reason, ok := GetErrorReason(err); !ok || reason != ReasonSendFailed {
				t.Fatalf("reason = %v, %v", reason, ok)
			}
			if calls != 0 {
				t.Fatalf("unsafe endpoint reached transport %d times", calls)
			}
		})
	}
}

func TestHTTPSameOriginRedirectStripsCredentials(t *testing.T) {
	var authorization, apiKey, idempotency, method, body string
	var readErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/send" {
			http.Redirect(w, r, "/delivery", http.StatusTemporaryRedirect)
			return
		}
		authorization, apiKey = r.Header.Get("Authorization"), r.Header.Get("X-Provider-Key")
		idempotency, method = r.Header.Get("Idempotency-Key"), r.Method
		var data []byte
		data, readErr = io.ReadAll(r.Body)
		body = string(data)
		_, _ = io.WriteString(w, `{"ok":true,"message_id":"delivered"}`)
	}))
	defer srv.Close()
	p, err := NewHTTPProvider(&HTTPConfig{
		BaseURL: srv.URL, SendEndpoint: "/send", Redirect: RedirectSameOrigin,
		APIKey: "secret", APIKeyHeader: "X-Provider-Key", Headers: map[string]string{"Authorization": "Bearer secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Send(context.Background(), NewMessage("+15551234567").WithCode("123456").WithIdempotencyKey("delivery-key"))
	if err != nil || !result.OK || result.MessageID != "delivered" {
		t.Fatalf("Send = %+v, %v", result, err)
	}
	if authorization != "" || apiKey != "" {
		t.Fatalf("redirect forwarded credentials: Authorization=%q, API key=%q", authorization, apiKey)
	}
	if readErr != nil || method != http.MethodPost || idempotency != "delivery-key" || !strings.Contains(body, "123456") {
		t.Fatalf("redirect lost request: method=%q, key=%q, body=%q, err=%v", method, idempotency, body, readErr)
	}
}

func TestHTTPRedirectWithoutHistory(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://example.test/send", nil)
	req.Header.Set("Authorization", "Bearer original")
	if err := makeCheckRedirect(&HTTPConfig{Redirect: RedirectDeny})(req, nil); err != nil {
		t.Fatalf("initial request rejected: %v", err)
	}
	if req.Header.Get("Authorization") != "Bearer original" {
		t.Fatal("initial request credentials removed")
	}
}

type boundaryTransport func(*http.Request) (*http.Response, error)

func (f boundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingResponseBody struct {
	err    error
	closed bool
}

func (b *failingResponseBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failingResponseBody) Close() error             { b.closed = true; return nil }

func TestHTTPReadFailureClosesBodyAndPreservesCause(t *testing.T) {
	p, err := NewHTTPProvider(&HTTPConfig{BaseURL: "https://example.test", ProviderName: "test", ChannelType: ChannelSMS})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("response interrupted")
	body := &failingResponseBody{err: cause}
	p.SetHTTPClient(&http.Client{Transport: boundaryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})})
	result, err := p.Send(context.Background(), NewMessage("+15551234567").WithCode("123456"))
	if !errors.Is(err, cause) || result.OK || !body.closed {
		t.Fatalf("Send = %+v, %v; body closed = %v", result, err, body.closed)
	}
	if reason, ok := GetErrorReason(err); !ok || reason != ReasonSendFailed {
		t.Fatalf("reason = %v, %v", reason, ok)
	}
}

func TestHTTPStatusFailuresDoNotExposeResponseBody(t *testing.T) {
	for _, response := range []string{"secret upstream token", `{"ok":false}`, `{"ok":true,"error_code":"rate_limited","error_message":"secret"}`} {
		t.Run(response, func(t *testing.T) {
			p, err := NewHTTPProvider(&HTTPConfig{BaseURL: "https://example.test"})
			if err != nil {
				t.Fatal(err)
			}
			p.SetHTTPClient(&http.Client{Transport: boundaryTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})})
			result, err := p.Send(context.Background(), NewMessage("+15551234567").WithCode("123456"))
			if err == nil || result.OK || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Send = %+v, %v", result, err)
			}
			if reason, ok := GetErrorReason(err); !ok || reason != ReasonSendFailed {
				t.Fatalf("reason = %v, %v", reason, ok)
			}
		})
	}
}
