# provider-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/provider-kit.svg)](https://pkg.go.dev/github.com/soulteary/provider-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
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

store := provider.NewMemoryIdempotencyStore()
defer store.Close() // stops the cleanup goroutine

idempotentProvider := provider.NewIdempotentProvider(smtpProvider, &provider.IdempotencyConfig{
    Store: store,
    TTL:   5 * time.Minute,
})

registry.Register(idempotentProvider)

msg := provider.NewMessage("recipient@example.com").
    WithBody("Important message").
    WithIdempotencyKey("unique-key-123")

result, err := registry.Send(ctx, provider.ChannelEmail, msg)
```

A second send with the same key returns the recorded result instead of sending
again. `WrapWithIdempotency(provider, store, ttl)` is the shorthand.

#### Concurrency

Two callers can present the same idempotency key at the same time, so the claim
has to be atomic. A store that implements `Reserver` gets that:

```go
type Reserver interface {
    // Reserve claims key atomically and returns any already-recorded result
    // in the same operation. The token identifies this claim.
    Reserve(ctx context.Context, key string, ttl time.Duration) (string, *SendResult, error)
    // Finalize records the outcome against a claim.
    Finalize(ctx context.Context, key, token string, result *SendResult, ttl time.Duration) error
    // Abandon releases a claim that will never produce a result.
    Abandon(ctx context.Context, key, token string) error
}
```

`MemoryIdempotencyStore` implements it. For Redis, `Reserve` is
`SET key value NX PX ttl`.

A store that implements only `IdempotencyStore` still works, through a
check-then-act path — `Get`, send, `Set`. **That path does not hold under
concurrency**: two requests with the same key both miss, both call the provider,
and two messages go out. The window is as wide as the provider call.

Handle the concurrent case:

```go
result, err := registry.Send(ctx, provider.ChannelEmail, msg)
switch {
case errors.Is(err, provider.ErrSendInFlight):
    // Another caller holds the claim and has not recorded a result yet.
    // Retry shortly; do not send a second message.
case errors.Is(err, provider.ErrClaimSuperseded):
    // This claim was taken over. Re-read the outcome rather than resending.
case err != nil:
    return err
}
```

#### What is and is not reported

- **A store read failure before sending is returned.** Nothing has been sent at
  that point, so refusing is safe and you can retry. Previously a store error was
  treated as a cache miss, so an unavailable store silently switched idempotency
  off and the message went out again with nothing reported.
- **A failure to *record* the outcome is deliberately swallowed.** The message has
  already gone out; reporting an error there would push a caller that retries on
  error into sending it a second time. The cost is a later retry that is not
  deduplicated, which is strictly better than duplicating a send that succeeded.
- **A send that fails without producing a result abandons the claim**, so a retry
  is not blocked for the whole TTL.

#### Memory store caveats

`MemoryIdempotencyStore` deduplicates **within one process only**. Multiple
instances behind a load balancer each keep their own map, so the same key can send
once per instance — use a shared store (Redis) for a multi-instance deployment.

Call `Close()` when you are done with a store. It stops the cleanup goroutine,
which otherwise runs for the life of the process and keeps the store and all its
entries reachable.

`Get` and `Reserve` return a **copy** of the stored result, so one caller mutating
what it got back does not change what other callers see.

### HTTP API Provider

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

## Upgrade Notes (v1.7.0)

One interface, two sentinels and one method were added; nothing was removed.
Idempotent sending refuses in cases where it previously sent.

- **A store read failure now fails the send.** The check was
  `if result, found, err := p.store.Get(...); err == nil && found`, so a store
  error was indistinguishable from a cache miss: when the store was unavailable,
  **idempotency silently switched off and the message was sent again**, with
  nothing reported. Nothing has been sent when that read fails, so refusing is
  safe — handle the error and retry.
- **A concurrent caller gets `ErrSendInFlight` instead of a second send.**
  `IdempotentProvider.Send` was a check-then-act, so two requests carrying the
  same key both missed, both called the provider, and two messages went out — the
  case idempotency exists to prevent. **Add handling for `ErrSendInFlight` and
  `ErrClaimSuperseded`** to any caller that can present the same key twice
  concurrently.
- **`Reserver` is new.** A store implementing it claims a key atomically and
  returns any recorded result in the same operation. `MemoryIdempotencyStore`
  implements it. A store implementing only `IdempotencyStore` keeps the old
  check-then-act path, which does not hold under concurrency — if you wrote a
  custom store, implementing `Reserver` is what makes it safe.
- **`MemoryIdempotencyStore.Close()` is new, and you should call it.** The store
  started a cleanup goroutine with no way to stop it, so **every store ever created
  leaked a goroutine**, and that goroutine kept the store and its entries
  reachable.
- **`Get` and `Reserve` return a copy.** `Get` returned the stored `*SendResult`
  directly, shared by every caller that hit the key, so one caller mutating it
  changed what the others saw.
- **A failed send abandons its claim**, so a retry is not blocked for the whole
  TTL.
- **Requirements said Go 1.26**; `go.mod` requires `1.27.0`.

Unchanged on purpose: a failure to *record* an outcome is still swallowed, because
the message has already been sent and surfacing an error there would make a
retry-on-error caller send it twice.

## Requirements

- **Go 1.27+** (`go.mod` declares `go 1.27.0`)

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
