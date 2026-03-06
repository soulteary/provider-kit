package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Function variables for testing
var (
	smtpSendMail  = smtp.SendMail
	tlsDial       = tls.Dial
	smtpDial      = smtp.Dial
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
	if c.Port <= 0 || c.Port > 65535 {
		return ErrInvalidConfig("SMTP port is invalid")
	}
	if c.From == "" {
		return ErrInvalidConfig("SMTP from address is required")
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

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &SMTPProvider{
		config: config,
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
	if err := msg.Validate(); err != nil {
		return NewFailureResult(p.name, ChannelEmail, NormalizeError(err, ChannelEmail, p.name)), err
	}

	// Build email message
	emailBody := p.buildEmailBody(msg)

	// Get SMTP address
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)

	// Send email based on TLS configuration
	var err error
	if p.config.UseTLS {
		err = p.sendWithTLS(addr, msg.To, emailBody)
	} else if p.config.UseStartTLS {
		err = p.sendWithStartTLS(addr, msg.To, emailBody)
	} else {
		err = p.sendPlain(addr, msg.To, emailBody)
	}

	if err != nil {
		providerErr := ErrSendFailed("failed to send email", err).WithProvider(p.name, ChannelEmail)
		return NewFailureResult(p.name, ChannelEmail, providerErr), providerErr
	}

	// Generate message ID
	messageID := uuid.New().String()
	if msg.IdempotencyKey != "" {
		messageID = msg.IdempotencyKey
	}

	return NewSuccessResult(p.name, ChannelEmail, messageID), nil
}

// buildEmailBody constructs the email body with headers
func (p *SMTPProvider) buildEmailBody(msg *Message) []byte {
	var sb strings.Builder

	// From header
	if p.config.FromName != "" {
		fmt.Fprintf(&sb, "From: %s <%s>\r\n", p.config.FromName, p.config.From)
	} else {
		fmt.Fprintf(&sb, "From: %s\r\n", p.config.From)
	}

	// To header
	fmt.Fprintf(&sb, "To: %s\r\n", msg.To)

	// Subject header
	if msg.Subject != "" {
		fmt.Fprintf(&sb, "Subject: %s\r\n", msg.Subject)
	}

	// Content-Type header
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")

	// Message-ID header
	if msg.IdempotencyKey != "" {
		fmt.Fprintf(&sb, "Message-ID: <%s@%s>\r\n", msg.IdempotencyKey, p.config.Host)
	}

	// Empty line before body
	sb.WriteString("\r\n")

	// Body
	sb.WriteString(msg.Body)

	return []byte(sb.String())
}

// sendPlain sends email without encryption
func (p *SMTPProvider) sendPlain(addr, to string, body []byte) error {
	var auth smtp.Auth
	if p.config.Username != "" {
		auth = smtp.PlainAuth("", p.config.Username, p.config.Password, p.config.Host)
	}
	return smtpSendMail(addr, auth, p.config.From, []string{to}, body)
}

// sendWithTLS sends email over TLS
func (p *SMTPProvider) sendWithTLS(addr, to string, body []byte) error {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.config.SkipTLSVerify,
		ServerName:         p.config.Host,
	}

	conn, err := tlsDial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client, err := smtpNewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	return p.sendWithClient(client, to, body)
}

// sendWithStartTLS sends email using STARTTLS
func (p *SMTPProvider) sendWithStartTLS(addr, to string, body []byte) error {
	client, err := smtpDial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Send STARTTLS command
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.config.SkipTLSVerify,
		ServerName:         p.config.Host,
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("STARTTLS failed: %w", err)
	}

	return p.sendWithClient(client, to, body)
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
	if err := client.Mail(p.config.From); err != nil {
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
