# provider-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/provider-kit.svg)](https://pkg.go.dev/github.com/soulteary/provider-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/provider-kit)](https://goreportcard.com/report/github.com/soulteary/provider-kit)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/provider-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/provider-kit)

[中文文档](README_CN.md)

A lightweight, extensible Go library for sending messages (email, SMS, DingTalk, etc.) through pluggable providers with built-in retry, idempotency, and error normalization.

## Features

- **Provider Interface** - Clean interface for implementing custom message providers
- **Registry Pattern** - Centralized provider management with channel-based routing
- **Built-in Providers** - SMTP for email, HTTP API for external services (SMS, DingTalk, etc.)
- **Automatic Retry** - Configurable retry logic with exponential backoff
- **Idempotency Support** - Prevent duplicate sends with idempotency keys
- **Error Normalization** - Unified error handling across different providers
- **Template Support** - Simple template rendering with multi-language support
- **Factory Pattern** - Create providers from configuration maps

## Installation

```bash
go get github.com/soulteary/provider-kit
```

## Quick Start

### Basic Usage

```go
import provider "github.com/soulteary/provider-kit"

// Create a registry
registry := provider.NewRegistry()

// Create and register an SMTP provider
smtpProvider, err := provider.NewSMTPProvider(&provider.SMTPConfig{
    Host:     "smtp.example.com",
    Port:     587,
    Username: "user@example.com",
    Password: "password",
    From:     "noreply@example.com",
})
if err != nil {
    log.Fatal(err)
}

if err := registry.Register(smtpProvider); err != nil {
    log.Fatal(err)
}

// Create and send a message
msg := provider.NewMessage("recipient@example.com").
    WithSubject("Hello").
    WithBody("This is a test email.")

result, err := registry.Send(context.Background(), provider.ChannelEmail, msg)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Message sent: %s\n", result.MessageID)
```

### With Retry Support

```go
import provider "github.com/soulteary/provider-kit"

// Wrap provider with retry
retryProvider := provider.NewRetryProvider(smtpProvider, &provider.RetryConfig{
    MaxRetries:        3,
    RetryDelay:        100 * time.Millisecond,
    MaxRetryDelay:     5 * time.Second,
    BackoffMultiplier: 2.0,
    RetryableReasons: []provider.ErrorReason{
        provider.ReasonProviderDown,
        provider.ReasonTimeout,
        provider.ReasonRateLimited,
    },
})

registry.Register(retryProvider)
```

### With Idempotency Support

```go
import provider "github.com/soulteary/provider-kit"

// Wrap provider with idempotency
idempotentProvider := provider.NewIdempotentProvider(smtpProvider, &provider.IdempotencyConfig{
    Store: provider.NewMemoryIdempotencyStore(),
    TTL:   5 * time.Minute,
})

registry.Register(idempotentProvider)

// Send with idempotency key
msg := provider.NewMessage("recipient@example.com").
    WithBody("Important message").
    WithIdempotencyKey("unique-key-123")

// Second send with same key will return cached result
result, _ := registry.Send(ctx, provider.ChannelEmail, msg)
```

### HTTP API Provider

```go
import provider "github.com/soulteary/provider-kit"

// Create HTTP provider for external SMS API
httpProvider, err := provider.NewHTTPProvider(&provider.HTTPConfig{
    BaseURL:      "https://api.sms-provider.com",
    SendEndpoint: "/v1/send",
    APIKey:       "your-api-key",
    ChannelType:  provider.ChannelSMS,
    ProviderName: "sms-provider",
})
if err != nil {
    log.Fatal(err)
}

registry.Register(httpProvider)
```

### Verification Code Messages

```go
import provider "github.com/soulteary/provider-kit"

// Build verification message with locale support
msg := provider.BuildVerificationMessage(
    "user@example.com",
    "123456",
    "zh-CN",
    provider.ChannelEmail,
)

// Send verification email
result, err := registry.Send(ctx, provider.ChannelEmail, msg)
```

### Provider Factory

```go
import provider "github.com/soulteary/provider-kit"

// Register factory
registry.RegisterFactory("smtp", provider.SMTPProviderFactory)

// Create provider from config map
config := map[string]string{
    "host":     "smtp.example.com",
    "port":     "587",
    "username": "user",
    "password": "pass",
    "from":     "noreply@example.com",
}

smtpProvider, err := registry.CreateProvider("smtp", config)
```

## API Reference

### Channel Types

| Channel | Description |
|---------|-------------|
| `ChannelSMS` | SMS message channel |
| `ChannelEmail` | Email message channel |
| `ChannelHTTP` | Generic HTTP API channel (for external SMS, DingTalk, etc.) |
| `ChannelDingTalk` | DingTalk work notification channel (via herald-dingtalk HTTP service) |

### Provider Interface

```go
type Provider interface {
    Send(ctx context.Context, msg *Message) (*SendResult, error)
    Channel() Channel
    Name() string
    Validate() error
}
```

### Message Options

| Method | Description |
|--------|-------------|
| `WithSubject(s)` | Set message subject (email) |
| `WithBody(s)` | Set message body |
| `WithTemplate(s)` | Set template name |
| `WithParams(map)` | Set template parameters |
| `WithCode(s)` | Set verification code |
| `WithLocale(s)` | Set locale for formatting |
| `WithIdempotencyKey(s)` | Set idempotency key |
| `AddMetadata(k, v)` | Add metadata entry |

### Error Reasons

| Reason | Description |
|--------|-------------|
| `ReasonSendFailed` | Send operation failed |
| `ReasonProviderDown` | Provider is unavailable |
| `ReasonInvalidConfig` | Invalid configuration |
| `ReasonRateLimited` | Rate limited by provider |
| `ReasonInvalidDestination` | Invalid recipient |
| `ReasonTimeout` | Request timed out |
| `ReasonUnauthorized` | Authentication failed |
| `ReasonNotRegistered` | No provider registered |
| `ReasonIdempotencyConflict` | Idempotency conflict |

### SMTP Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Host` | `string` | Required | SMTP server host |
| `Port` | `int` | `587` | SMTP server port |
| `Username` | `string` | `""` | Authentication username |
| `Password` | `string` | `""` | Authentication password |
| `From` | `string` | Required | Sender email address |
| `FromName` | `string` | `""` | Sender display name |
| `UseTLS` | `bool` | `false` | Use direct TLS connection |
| `UseStartTLS` | `bool` | `true` | Use STARTTLS |
| `SkipTLSVerify` | `bool` | `false` | Skip TLS verification |
| `Timeout` | `time.Duration` | `30s` | Connection timeout |

### Retry Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `MaxRetries` | `int` | `3` | Maximum retry attempts |
| `RetryDelay` | `time.Duration` | `100ms` | Initial retry delay |
| `MaxRetryDelay` | `time.Duration` | `5s` | Maximum retry delay |
| `BackoffMultiplier` | `float64` | `2.0` | Exponential backoff multiplier |
| `RetryableReasons` | `[]ErrorReason` | `[ProviderDown, Timeout, RateLimited]` | Errors that trigger retry |

## Project Structure

```
provider-kit/
├── channel.go          # Channel type definitions
├── channel_test.go
├── errors.go           # Error types and normalization
├── errors_test.go
├── http.go             # HTTP API provider
├── http_test.go
├── idempotency.go      # Idempotency support
├── idempotency_test.go
├── interface.go        # Provider interface definitions
├── message.go          # Message type
├── message_test.go
├── registry.go         # Provider registry
├── registry_test.go
├── result.go           # Send result type
├── result_test.go
├── retry.go            # Retry logic
├── retry_test.go
├── smtp.go             # SMTP provider
├── smtp_test.go
├── template.go         # Template utilities
├── template_test.go
├── go.mod
└── LICENSE
```

## Implementing Custom Providers

```go
type MyCustomProvider struct {
    config MyConfig
}

func (p *MyCustomProvider) Send(ctx context.Context, msg *provider.Message) (*provider.SendResult, error) {
    // Implement your send logic
    if err := p.doSend(msg); err != nil {
        return provider.NewFailureResult(p.Name(), p.Channel(), 
            provider.ErrSendFailed("send failed", err)), err
    }
    return provider.NewSuccessResult(p.Name(), p.Channel(), "msg-id"), nil
}

func (p *MyCustomProvider) Channel() provider.Channel {
    return provider.ChannelSMS
}

func (p *MyCustomProvider) Name() string {
    return "my-custom"
}

func (p *MyCustomProvider) Validate() error {
    if p.config.APIKey == "" {
        return provider.ErrInvalidConfig("API key is required")
    }
    return nil
}
```

## Test Coverage

Run tests with coverage:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Requirements

- Go 1.21 or later

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
