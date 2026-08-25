package provider

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Function variables for testing
var (
	smtpDialContext = func(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: timeout}
		return dialer.DialContext(ctx, network, address)
	}
	smtpNewClient = func(conn net.Conn, host string) (*smtp.Client, error) {
		return smtp.NewClient(conn, host)
	}
)

// SMTPConfig contains SMTP provider configuration
type SMTPConfig struct {
	// Host is the SMTP server host
	Host string
	// Port is the SMTP server port
	Port int
	// Username is the SMTP authentication username
	Username string
	// Password is the SMTP authentication password
	Password string
	// From is the sender email address
	From string
	// FromName is the sender display name (optional)
	FromName string
	// UseTLS enables TLS connection
	UseTLS bool
	// UseStartTLS enables STARTTLS
	UseStartTLS bool
	// SkipTLSVerify skips TLS certificate verification (not recommended)
	SkipTLSVerify bool
	// Timeout is the connection timeout
	Timeout time.Duration
}

// DefaultSMTPConfig returns default SMTP configuration
func DefaultSMTPConfig() *SMTPConfig {
	return &SMTPConfig{
		Port:        587,
		UseTLS:      false,
		Timeout:     30 * time.Second,
		UseStartTLS: true,
	}
}

// Validate validates the SMTP configuration
func (c *SMTPConfig) Validate() error {
	if c.Host == "" {
		return ErrInvalidConfig("SMTP host is required")
	}
	if strings.TrimSpace(c.Host) != c.Host || containsHeaderControl(c.Host) || strings.ContainsAny(c.Host, " \t") {
		return ErrInvalidConfig("SMTP host is invalid")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return ErrInvalidConfig("SMTP port is invalid")
	}
	if c.From == "" {
		return ErrInvalidConfig("SMTP from address is required")
	}
	if containsHeaderControl(c.From) {
		return ErrInvalidConfig("SMTP from address is invalid")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return ErrInvalidConfig("SMTP from address is invalid")
	}
	if containsHeaderControl(c.FromName) || !utf8.ValidString(c.FromName) {
		return ErrInvalidConfig("SMTP from name is invalid")
	}
	if c.Timeout < 0 {
		return ErrInvalidConfig("SMTP timeout must not be negative")
	}
	return nil
}

// SMTPDialer is the interface for SMTP connection operations
type SMTPDialer interface {
	// DialPlain dials SMTP server without encryption
	DialPlain(addr string, auth interface{}, from string, to []string, body []byte) error
	// DialTLS dials SMTP server with TLS
	DialTLS(addr string, tlsConfig interface{}, host string) (SMTPClient, error)
	// DialStartTLS dials SMTP server and upgrades to TLS
	DialStartTLS(addr string, tlsConfig interface{}) (SMTPClient, error)
}

// SMTPClient is the interface for SMTP client operations
type SMTPClient interface {
	Auth(a interface{}) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (interface {
		Write([]byte) (int, error)
		Close() error
	}, error)
	Quit() error
	Close() error
}

// DefaultSMTPDialer uses the standard library for SMTP
type DefaultSMTPDialer struct{}

// SMTPProvider implements email sending via SMTP
type SMTPProvider struct {
	config *SMTPConfig
	name   string
	dialer SMTPDialer // Optional custom dialer for testing
}

// NewSMTPProvider creates a new SMTP provider
func NewSMTPProvider(config *SMTPConfig) (*SMTPProvider, error) {
	if config == nil {
		config = DefaultSMTPConfig()
	}
	configCopy := *config
	if configCopy.Timeout == 0 {
		configCopy.Timeout = 30 * time.Second
	}

	if err := configCopy.Validate(); err != nil {
		return nil, err
	}

	return &SMTPProvider{
		config: &configCopy,
		name:   "smtp",
	}, nil
}

// NewSMTPProviderFromMap creates an SMTP provider from a configuration map
func NewSMTPProviderFromMap(configMap map[string]string) (Provider, error) {
	config := DefaultSMTPConfig()

	if host, ok := configMap["host"]; ok {
		config.Host = host
	}
	if port, ok := configMap["port"]; ok {
		_, _ = fmt.Sscanf(port, "%d", &config.Port)
	}
	if username, ok := configMap["username"]; ok {
		config.Username = username
	}
	if password, ok := configMap["password"]; ok {
		config.Password = password
	}
	if from, ok := configMap["from"]; ok {
		config.From = from
	}
	if fromName, ok := configMap["from_name"]; ok {
		config.FromName = fromName
	}
	if useTLS, ok := configMap["use_tls"]; ok {
		config.UseTLS = useTLS == "true" || useTLS == "1"
	}
	if useStartTLS, ok := configMap["use_starttls"]; ok {
		config.UseStartTLS = useStartTLS == "true" || useStartTLS == "1"
	}
	if skipVerify, ok := configMap["skip_tls_verify"]; ok {
		config.SkipTLSVerify = skipVerify == "true" || skipVerify == "1"
	}

	return NewSMTPProvider(config)
}

// Channel returns the channel type
func (p *SMTPProvider) Channel() Channel {
	return ChannelEmail
}

// Name returns the provider name
func (p *SMTPProvider) Name() string {
	return p.name
}

// Validate checks if the provider is properly configured
func (p *SMTPProvider) Validate() error {
	return p.config.Validate()
}

// SetDialer sets a custom dialer for testing
func (p *SMTPProvider) SetDialer(dialer SMTPDialer) {
	p.dialer = dialer
}

// Send sends an email via SMTP
func (p *SMTPProvider) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if msg == nil {
		err := ErrValidationFailed("message is required").WithProvider(p.name, ChannelEmail)
		return NewFailureResult(p.name, ChannelEmail, err), err
	}
	if err := msg.Validate(); err != nil {
		return NewFailureResult(p.name, ChannelEmail, NormalizeError(err, ChannelEmail, p.name)), err
	}
	to, err := parseRecipient(msg.To)
	if err != nil {
		providerErr := ErrInvalidDestination("recipient email address is invalid").WithProvider(p.name, ChannelEmail).WithError(err)
		return NewFailureResult(p.name, ChannelEmail, providerErr), providerErr
	}
	if err := validateHeaderValue("subject", msg.Subject); err != nil {
		providerErr := ErrValidationFailed(err.Error()).WithProvider(p.name, ChannelEmail)
		return NewFailureResult(p.name, ChannelEmail, providerErr), providerErr
	}

	wireMessageID := uuid.New().String()
	emailBody := p.buildEmailBody(msg, to, wireMessageID)

	// Get SMTP address
	addr := net.JoinHostPort(p.config.Host, strconv.Itoa(p.config.Port))

	// Send email based on TLS configuration
	var sendErr error
	if p.config.UseTLS {
		sendErr = p.sendWithTLS(ctx, addr, to.Address, emailBody)
	} else if p.config.UseStartTLS {
		sendErr = p.sendWithStartTLS(ctx, addr, to.Address, emailBody)
	} else {
		sendErr = p.sendPlain(ctx, addr, to.Address, emailBody)
	}

	if sendErr != nil {
		providerErr := p.normalizeSendError(ctx, sendErr)
		return NewFailureResult(p.name, ChannelEmail, providerErr), providerErr
	}

	resultMessageID := wireMessageID
	if msg.IdempotencyKey != "" {
		// Preserve the existing SendResult contract while keeping caller input out
		// of the RFC 5322 Message-ID header.
		resultMessageID = msg.IdempotencyKey
	}
	return NewSuccessResult(p.name, ChannelEmail, resultMessageID), nil
}

// buildEmailBody constructs the email body with headers
func (p *SMTPProvider) buildEmailBody(msg *Message, to *mail.Address, messageID string) []byte {
	var sb strings.Builder
	from, _ := mail.ParseAddress(p.config.From)

	// From header
	if p.config.FromName != "" {
		from.Name = p.config.FromName
		fmt.Fprintf(&sb, "From: %s\r\n", formatAddress(from))
	} else {
		fmt.Fprintf(&sb, "From: %s\r\n", formatAddress(from))
	}

	// To header
	fmt.Fprintf(&sb, "To: %s\r\n", formatAddress(to))

	// Subject header
	if msg.Subject != "" {
		fmt.Fprintf(&sb, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", msg.Subject))
	}

	sb.WriteString("MIME-Version: 1.0\r\n")
	// Content-Type header
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")

	// Message-ID header
	fmt.Fprintf(&sb, "Message-ID: <%s@%s>\r\n", messageID, messageIDDomain(from.Address))

	// Empty line before body
	sb.WriteString("\r\n")

	// Body
	sb.WriteString(msg.Body)

	return []byte(sb.String())
}

// sendPlain sends email without encryption
func (p *SMTPProvider) sendPlain(ctx context.Context, addr, to string, body []byte) error {
	conn, cleanup, err := p.dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("SMTP dial failed: %w", err)
	}
	defer cleanup()

	client, err := smtpNewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	return p.sendWithClient(client, to, body)
}

// sendWithTLS sends email over TLS
func (p *SMTPProvider) sendWithTLS(ctx context.Context, addr, to string, body []byte) error {
	conn, cleanup, err := p.dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}
	defer cleanup()

	tlsConn := tls.Client(conn, p.tlsConfig())
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	client, err := smtpNewClient(tlsConn, p.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	return p.sendWithClient(client, to, body)
}

// sendWithStartTLS sends email using STARTTLS
func (p *SMTPProvider) sendWithStartTLS(ctx context.Context, addr, to string, body []byte) error {
	conn, cleanup, err := p.dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("SMTP dial failed: %w", err)
	}
	defer cleanup()

	client, err := smtpNewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Send STARTTLS command
	if err := client.StartTLS(p.tlsConfig()); err != nil {
		return fmt.Errorf("STARTTLS failed: %w", err)
	}

	return p.sendWithClient(client, to, body)
}

func (p *SMTPProvider) dial(ctx context.Context, addr string) (net.Conn, func(), error) {
	conn, err := smtpDialContext(ctx, "tcp", addr, p.config.Timeout)
	if err != nil {
		return nil, nil, err
	}

	deadline := time.Now().Add(p.config.Timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	cleanup := func() {
		stopCancel()
		_ = conn.Close()
	}
	return conn, cleanup, nil
}

func (p *SMTPProvider) tlsConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: p.config.SkipTLSVerify,
		ServerName:         p.config.Host,
		MinVersion:         tls.VersionTLS12,
	}
}

func (p *SMTPProvider) normalizeSendError(ctx context.Context, err error) *ProviderError {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return ErrTimeout("SMTP send timed out", err).WithProvider(p.name, ChannelEmail)
	}
	if errors.Is(err, context.Canceled) {
		return ErrSendFailed("SMTP send canceled", err).WithProvider(p.name, ChannelEmail)
	}
	return ErrSendFailed("failed to send email", err).WithProvider(p.name, ChannelEmail)
}

func parseRecipient(raw string) (*mail.Address, error) {
	if containsHeaderControl(raw) {
		return nil, fmt.Errorf("recipient contains prohibited control characters")
	}
	address, err := mail.ParseAddress(raw)
	if err != nil || address.Address == "" {
		return nil, fmt.Errorf("invalid recipient email address")
	}
	return address, nil
}

func validateHeaderValue(field, value string) error {
	if containsHeaderControl(value) {
		return fmt.Errorf("%s contains prohibited control characters", field)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	return nil
}

func containsHeaderControl(value string) bool {
	return strings.ContainsAny(value, "\r\n\x00")
}

func formatAddress(address *mail.Address) string {
	if address.Name == "" {
		return address.Address
	}
	return address.String()
}

func messageIDDomain(from string) string {
	if at := strings.LastIndexByte(from, '@'); at >= 0 && at+1 < len(from) {
		return from[at+1:]
	}
	return "localhost"
}

// smtpClientInterface defines the interface for SMTP client operations
type smtpClientInterface interface {
	Auth(a smtp.Auth) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

// sendWithClient sends email using an established SMTP client
func (p *SMTPProvider) sendWithClient(client smtpClientInterface, to string, body []byte) error {
	// Authenticate if credentials provided
	if p.config.Username != "" {
		auth := smtp.PlainAuth("", p.config.Username, p.config.Password, p.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Set sender
	from, _ := mail.ParseAddress(p.config.From)
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("MAIL FROM failed: %w", err)
	}

	// Set recipient
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO failed: %w", err)
	}

	// Send body
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command failed: %w", err)
	}

	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("write body failed: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer failed: %w", err)
	}

	return client.Quit()
}

// SMTPProviderFactory is the factory for SMTP providers
func SMTPProviderFactory(config map[string]string) (Provider, error) {
	return NewSMTPProviderFromMap(config)
}
