package provider

import (
	"fmt"
	"strings"
)

// Message represents a message to be sent via a provider
type Message struct {
	// To is the recipient address (email address or phone number)
	To string

	// Subject is the message subject (primarily for email)
	Subject string

	// Body is the message body content
	Body string

	// Template is the template name/identifier (for templated messages)
	Template string

	// Params are template parameters/variables
	Params map[string]string

	// Code is the verification code (for OTP messages)
	Code string

	// Locale is the locale for message formatting (e.g., "en-US", "zh-CN")
	Locale string

	// IdempotencyKey is the key for idempotency (prevents duplicate sends)
	IdempotencyKey string

	// Metadata contains additional provider-specific data
	Metadata map[string]string
}

// NewMessage creates a new Message with the required destination
func NewMessage(to string) *Message {
	return &Message{
		To:       to,
		Params:   make(map[string]string),
		Metadata: make(map[string]string),
	}
}

// WithSubject sets the message subject
func (m *Message) WithSubject(subject string) *Message {
	m.Subject = subject
	return m
}

// WithBody sets the message body
func (m *Message) WithBody(body string) *Message {
	m.Body = body
	return m
}

// WithTemplate sets the template name
func (m *Message) WithTemplate(template string) *Message {
	m.Template = template
	return m
}

// WithParams sets the template parameters
func (m *Message) WithParams(params map[string]string) *Message {
	m.Params = params
	return m
}

// WithParam adds a single template parameter
func (m *Message) WithParam(key, value string) *Message {
	if m.Params == nil {
		m.Params = make(map[string]string)
	}
	m.Params[key] = value
	return m
}

// WithCode sets the verification code
func (m *Message) WithCode(code string) *Message {
	m.Code = code
	return m
}

// WithLocale sets the locale
func (m *Message) WithLocale(locale string) *Message {
	m.Locale = locale
	return m
}

// WithIdempotencyKey sets the idempotency key
func (m *Message) WithIdempotencyKey(key string) *Message {
	m.IdempotencyKey = key
	return m
}

// WithMetadata sets additional metadata
func (m *Message) WithMetadata(metadata map[string]string) *Message {
	m.Metadata = metadata
	return m
}

// AddMetadata adds a single metadata entry
func (m *Message) AddMetadata(key, value string) *Message {
	if m.Metadata == nil {
		m.Metadata = make(map[string]string)
	}
	m.Metadata[key] = value
	return m
}

// Validate validates the message has required fields
func (m *Message) Validate() error {
	if m.To == "" {
		return ErrInvalidDestination("destination (To) is required")
	}
	if err := validateNoCRLF("To", m.To); err != nil {
		return err
	}
	if err := validateNoCRLF("Subject", m.Subject); err != nil {
		return err
	}
	if err := validateNoCRLF("IdempotencyKey", m.IdempotencyKey); err != nil {
		return err
	}
	return nil
}

// Clone creates a deep copy of the message
func (m *Message) Clone() *Message {
	if m == nil {
		return nil
	}

	clone := &Message{
		To:             m.To,
		Subject:        m.Subject,
		Body:           m.Body,
		Template:       m.Template,
		Code:           m.Code,
		Locale:         m.Locale,
		IdempotencyKey: m.IdempotencyKey,
	}

	if m.Params != nil {
		clone.Params = make(map[string]string, len(m.Params))
		for k, v := range m.Params {
			clone.Params[k] = v
		}
	}

	if m.Metadata != nil {
		clone.Metadata = make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			clone.Metadata[k] = v
		}
	}

	return clone
}

// String returns a string representation (safe for logging, without sensitive data)
func (m *Message) String() string {
	return fmt.Sprintf("Message{To: %s, Template: %s, Locale: %s, HasCode: %v}",
		maskDestination(m.To), m.Template, m.Locale, m.Code != "")
}

// maskDestination masks a destination for safe logging
func maskDestination(dest string) string {
	if len(dest) <= 4 {
		return "****"
	}
	// For email: show first 2 chars and domain
	for i, c := range dest {
		if c == '@' && i > 2 {
			return dest[:2] + "***" + dest[i:]
		}
	}
	// For phone: show last 4 digits
	return "***" + dest[len(dest)-4:]
}

func validateNoCRLF(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return ErrValidationFailed(fmt.Sprintf("%s contains invalid control characters", field))
	}
	return nil
}
