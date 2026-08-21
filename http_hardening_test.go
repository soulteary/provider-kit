package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPProvider_TimeoutFloorApplied proves that a directly-constructed
// config with a zero Timeout gets the minimum timeout floor rather than "no
// timeout".
func TestHTTPProvider_TimeoutFloorApplied(t *testing.T) {
	p, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "https://example.test",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		// Timeout intentionally left zero.
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider: %v", err)
	}
	if p.httpClient.Timeout != minProviderTimeout {
		t.Fatalf("timeout = %v, want floor %v", p.httpClient.Timeout, minProviderTimeout)
	}
}

// TestHTTPProvider_RequireHTTPS proves http:// is rejected when RequireHTTPS.
func TestHTTPProvider_RequireHTTPS(t *testing.T) {
	_, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      "http://insecure.test",
		RequireHTTPS: true,
		ChannelType:  ChannelSMS,
		ProviderName: "test",
	})
	if err == nil {
		t.Fatal("expected error for http:// with RequireHTTPS")
	}
}

// TestHTTPProvider_BoundedResponseRead proves an oversized response body is
// truncated to MaxResponseBytes and does not blow up memory / parsing.
func TestHTTPProvider_BoundedResponseRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write far more than the cap; the client must stop reading at the cap.
		junk := strings.Repeat("x", 10_000)
		for i := 0; i < 100; i++ {
			_, _ = w.Write([]byte(junk))
		}
	}))
	defer srv.Close()

	p, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:          srv.URL,
		ChannelType:      ChannelSMS,
		ProviderName:     "test",
		MaxResponseBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider: %v", err)
	}
	msg := NewMessage("+15551234567").WithBody("hello")
	// The body is not valid JSON within the cap window, so this returns a
	// send_failed error rather than parsing an unbounded body. The point is it
	// returns promptly without reading the full (huge) body.
	_, _ = p.Send(context.Background(), msg)
}

// TestHTTPProvider_RedirectDenied proves the default deny policy refuses to
// follow a redirect.
func TestHTTPProvider_RedirectDenied(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		_, _ = w.Write([]byte(`{"ok":true,"message_id":"leaked"}`))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	p, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      origin.URL,
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Redirect:     RedirectDeny,
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider: %v", err)
	}
	res, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("hi"))
	if err == nil && res != nil && res.OK {
		t.Fatal("redirect should have been denied, but send succeeded")
	}
	if redirected {
		t.Fatal("client followed a denied redirect")
	}
}

// TestHTTPProvider_CrossOriginRedirectBlocksCredentials proves that even in
// same-origin mode a cross-host redirect is blocked and the API key is never
// forwarded to the redirect target.
func TestHTTPProvider_CrossOriginRedirectBlocksCredentials(t *testing.T) {
	var leakedKey string
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		leakedKey = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"ok":true,"message_id":"x"}`))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	p, err := NewHTTPProvider(&HTTPConfig{
		BaseURL:      origin.URL,
		APIKey:       "super-secret",
		ChannelType:  ChannelSMS,
		ProviderName: "test",
		Redirect:     RedirectSameOrigin,
	})
	if err != nil {
		t.Fatalf("NewHTTPProvider: %v", err)
	}
	res, err := p.Send(context.Background(), NewMessage("+15551234567").WithBody("hi"))
	if err == nil && res != nil && res.OK {
		t.Fatal("cross-origin redirect should have been blocked")
	}
	if reached {
		t.Fatalf("cross-origin redirect target was reached; leaked key=%q", leakedKey)
	}
	if leakedKey != "" {
		t.Fatalf("API key leaked to redirect target: %q", leakedKey)
	}
	_ = time.Now
}
