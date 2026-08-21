package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RedirectPolicy controls how the HTTP provider handles 3xx redirects.
type RedirectPolicy string

const (
	// RedirectDeny refuses to follow any redirect (safest default).
	RedirectDeny RedirectPolicy = "deny"
	// RedirectSameOrigin follows a redirect only when scheme+host are identical
	// to the original request; credentials are never re-sent on redirect.
	RedirectSameOrigin RedirectPolicy = "same-origin"
)

// defaultMaxResponseBytes bounds how much of a provider response body we will
// read, preventing a hostile or misbehaving provider from exhausting memory.
const defaultMaxResponseBytes int64 = 1 << 20 // 1 MiB

// minProviderTimeout is a floor applied when a caller constructs HTTPConfig
// directly and leaves Timeout at its zero value (which would otherwise mean "no
// timeout" and allow a provider that never responds to hang a request forever).
const minProviderTimeout = 30 * time.Second

// HTTPConfig contains HTTP API provider configuration
type HTTPConfig struct {
	// BaseURL is the API base URL
	BaseURL string
	// SendEndpoint is the send endpoint path (default: /v1/send)
	SendEndpoint string
	// APIKey is the API key for authentication
	APIKey string
	// APIKeyHeader is the header name for API key (default: X-API-Key)
	APIKeyHeader string
	// Timeout is the request timeout
	Timeout time.Duration
	// Headers are additional headers to include in requests
	Headers map[string]string
	// ChannelType is the channel type this provider handles
	ChannelType Channel
	// ProviderName is the name of this provider instance
	ProviderName string
	// MaxResponseBytes bounds the response body read (default 1 MiB). A value
	// <= 0 uses the default.
	MaxResponseBytes int64
	// Redirect controls redirect handling (default: deny).
	Redirect RedirectPolicy
	// RequireHTTPS rejects non-https BaseURLs at construction time.
	RequireHTTPS bool
}

// DefaultHTTPConfig returns default HTTP configuration
func DefaultHTTPConfig() *HTTPConfig {
	return &HTTPConfig{
		SendEndpoint:     "/v1/send",
		APIKeyHeader:     "X-API-Key",
		Timeout:          30 * time.Second,
		Headers:          make(map[string]string),
		ChannelType:      ChannelHTTP,
		ProviderName:     "http",
		MaxResponseBytes: defaultMaxResponseBytes,
		Redirect:         RedirectDeny,
	}
}

// Validate validates the HTTP configuration. It parses BaseURL and rejects
// unsupported schemes so a plaintext or malformed URL fails fast at
// construction time instead of at first send.
func (c *HTTPConfig) Validate() error {
	if c.BaseURL == "" {
		return ErrInvalidConfig("HTTP base URL is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ErrInvalidConfig("HTTP base URL is not a valid URL: " + err.Error())
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidConfig("HTTP base URL must use http or https scheme")
	}
	if u.Host == "" {
		return ErrInvalidConfig("HTTP base URL must include a host")
	}
	if c.RequireHTTPS && u.Scheme != "https" {
		return ErrInvalidConfig("HTTP base URL must use https scheme")
	}
	return nil
}

// HTTPProvider implements message sending via HTTP API
// This is a generic provider for external messaging APIs
type HTTPProvider struct {
	config     *HTTPConfig
	httpClient *http.Client
}

// NewHTTPProvider creates a new HTTP API provider
func NewHTTPProvider(config *HTTPConfig) (*HTTPProvider, error) {
	if config == nil {
		config = DefaultHTTPConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Enforce a timeout floor: a zero Timeout on http.Client means "no timeout",
	// which lets a provider that never responds hang the caller indefinitely.
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = minProviderTimeout
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.Redirect == "" {
		config.Redirect = RedirectDeny
	}
	if config.APIKeyHeader == "" {
		config.APIKeyHeader = "X-API-Key"
	}

	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: makeCheckRedirect(config),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	return &HTTPProvider{
		config:     config,
		httpClient: client,
	}, nil
}

// makeCheckRedirect returns a CheckRedirect function enforcing the configured
// redirect policy. It never forwards the API key / Authorization headers across
// a redirect and blocks cross-origin or scheme-downgrade redirects.
func makeCheckRedirect(config *HTTPConfig) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		if config.Redirect == RedirectDeny {
			return errRedirectBlocked
		}
		orig := via[0].URL
		// same-origin: scheme AND host must match the original request, and we
		// never downgrade https->http.
		if !strings.EqualFold(req.URL.Scheme, orig.Scheme) || !strings.EqualFold(req.URL.Host, orig.Host) {
			return errRedirectBlocked
		}
		// Defense in depth: strip any credential headers on the redirected
		// request even for a same-origin redirect.
		req.Header.Del("Authorization")
		if config.APIKeyHeader != "" {
			req.Header.Del(config.APIKeyHeader)
		}
		return nil
	}
}

var errRedirectBlocked = errors.New("provider redirect blocked by policy")

// NewHTTPProviderFromMap creates an HTTP provider from a configuration map
func NewHTTPProviderFromMap(configMap map[string]string) (Provider, error) {
	config := DefaultHTTPConfig()

	if baseURL, ok := configMap["base_url"]; ok {
		config.BaseURL = baseURL
	}
	if endpoint, ok := configMap["send_endpoint"]; ok {
		config.SendEndpoint = endpoint
	}
	if apiKey, ok := configMap["api_key"]; ok {
		config.APIKey = apiKey
	}
	if apiKeyHeader, ok := configMap["api_key_header"]; ok {
		config.APIKeyHeader = apiKeyHeader
	}
	if channelType, ok := configMap["channel"]; ok {
		config.ChannelType = Channel(channelType)
	}
	if providerName, ok := configMap["name"]; ok {
		config.ProviderName = providerName
	}

	return NewHTTPProvider(config)
}

// Channel returns the channel type
func (p *HTTPProvider) Channel() Channel {
	return p.config.ChannelType
}

// Name returns the provider name
func (p *HTTPProvider) Name() string {
	return p.config.ProviderName
}

// Validate checks if the provider is properly configured
func (p *HTTPProvider) Validate() error {
	return p.config.Validate()
}

// HTTPSendRequest is the request body for the HTTP send API
type HTTPSendRequest struct {
	Channel        string            `json:"channel"`
	To             string            `json:"to"`
	Template       string            `json:"template,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	Subject        string            `json:"subject,omitempty"`
	Body           string            `json:"body,omitempty"`
	Locale         string            `json:"locale,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

// HTTPSendResponse is the response body from the HTTP send API
type HTTPSendResponse struct {
	OK           bool   `json:"ok"`
	MessageID    string `json:"message_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Send sends a message via HTTP API
func (p *HTTPProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if err := msg.Validate(); err != nil {
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, NormalizeError(err, p.config.ChannelType, p.config.ProviderName)), err
	}

	// Generate idempotency key if not provided
	idempotencyKey := msg.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = uuid.New().String()
	}

	// Build request body
	reqBody := HTTPSendRequest{
		Channel:        string(p.config.ChannelType),
		To:             msg.To,
		Template:       msg.Template,
		Params:         msg.Params,
		Subject:        msg.Subject,
		Body:           msg.Body,
		Locale:         msg.Locale,
		IdempotencyKey: idempotencyKey,
	}

	// Serialize request
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		providerErr := ErrSendFailed("failed to serialize request", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}

	// Build HTTP request. Resolve the endpoint against the parsed base URL so a
	// stray/missing slash or an absolute endpoint cannot silently retarget the
	// request to a different host.
	base, err := url.Parse(p.config.BaseURL)
	if err != nil {
		providerErr := ErrSendFailed("invalid base URL", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}
	ref, err := url.Parse(p.config.SendEndpoint)
	if err != nil {
		providerErr := ErrSendFailed("invalid send endpoint", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}
	resolved := base.ResolveReference(ref)
	// A relative send endpoint must not change the origin.
	if !strings.EqualFold(resolved.Scheme, base.Scheme) || !strings.EqualFold(resolved.Host, base.Host) {
		providerErr := ErrSendFailed("send endpoint resolves to a different origin", nil).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolved.String(), bytes.NewReader(jsonBody))
	if err != nil {
		providerErr := ErrSendFailed("failed to create request", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	if p.config.APIKey != "" {
		req.Header.Set(p.config.APIKeyHeader, p.config.APIKey)
	}
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		providerErr := ErrProviderDown("HTTP request failed", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body with a hard cap so a hostile/oversized response cannot
	// exhaust memory.
	limit := p.config.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		providerErr := ErrSendFailed("failed to read response", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}

	// Parse response
	var sendResp HTTPSendResponse
	if err := json.Unmarshal(respBody, &sendResp); err != nil {
		// If we can't parse the response, check HTTP status
		if resp.StatusCode >= 400 {
			providerErr := ErrSendFailed(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)), nil).WithProvider(p.config.ProviderName, p.config.ChannelType)
			return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
		}
		providerErr := ErrSendFailed("failed to parse response", err).WithProvider(p.config.ProviderName, p.config.ChannelType)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}

	// Check response status
	if !sendResp.OK {
		providerErr := p.mapErrorCode(sendResp.ErrorCode, sendResp.ErrorMessage)
		return NewFailureResult(p.config.ProviderName, p.config.ChannelType, providerErr), providerErr
	}

	// Success
	result := NewSuccessResult(p.config.ProviderName, p.config.ChannelType, sendResp.MessageID)
	if sendResp.Provider != "" {
		result.WithMetadata("upstream_provider", sendResp.Provider)
	}
	return result, nil
}

// mapErrorCode maps error codes from the HTTP API to ProviderError
func (p *HTTPProvider) mapErrorCode(code, message string) *ProviderError {
	var reason ErrorReason
	switch code {
	case "rate_limited":
		reason = ReasonRateLimited
	case "invalid_destination":
		reason = ReasonInvalidDestination
	case "timeout":
		reason = ReasonTimeout
	case "unauthorized":
		reason = ReasonUnauthorized
	case "provider_down":
		reason = ReasonProviderDown
	case "idempotency_conflict":
		reason = ReasonIdempotencyConflict
	default:
		reason = ReasonSendFailed
	}

	return &ProviderError{
		Reason:       reason,
		Message:      message,
		ProviderName: p.config.ProviderName,
		Channel:      p.config.ChannelType,
	}
}

// SetHTTPClient sets a custom HTTP client
func (p *HTTPProvider) SetHTTPClient(client *http.Client) {
	p.httpClient = client
}

// HTTPProviderFactory is the factory for HTTP providers
func HTTPProviderFactory(config map[string]string) (Provider, error) {
	return NewHTTPProviderFromMap(config)
}
