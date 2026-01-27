# provider-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/provider-kit.svg)](https://pkg.go.dev/github.com/soulteary/provider-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/provider-kit)](https://goreportcard.com/report/github.com/soulteary/provider-kit)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/provider-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/provider-kit)

[English](README.md)

一个轻量级、可扩展的 Go 消息发送库，支持插件化 Provider、自动重试、幂等性和错误归一化。

## 功能特性

- **Provider 接口** - 清晰的接口定义，便于实现自定义消息提供者
- **Registry 模式** - 基于通道类型的集中式 Provider 管理
- **内置 Provider** - SMTP 邮件发送、HTTP API 外部服务
- **自动重试** - 可配置的重试逻辑，支持指数退避
- **幂等性支持** - 通过幂等键防止重复发送
- **错误归一化** - 跨不同 Provider 的统一错误处理
- **模板支持** - 简单的模板渲染，支持多语言
- **工厂模式** - 从配置 Map 创建 Provider

## 安装

```bash
go get github.com/soulteary/provider-kit
```

## 快速开始

### 基础用法

```go
import provider "github.com/soulteary/provider-kit"

// 创建 Registry
registry := provider.NewRegistry()

// 创建并注册 SMTP Provider
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

// 创建并发送消息
msg := provider.NewMessage("recipient@example.com").
    WithSubject("你好").
    WithBody("这是一封测试邮件。")

result, err := registry.Send(context.Background(), provider.ChannelEmail, msg)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("消息已发送: %s\n", result.MessageID)
```

### 带重试支持

```go
import provider "github.com/soulteary/provider-kit"

// 用重试包装 Provider
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

### 带幂等性支持

```go
import provider "github.com/soulteary/provider-kit"

// 用幂等性包装 Provider
idempotentProvider := provider.NewIdempotentProvider(smtpProvider, &provider.IdempotencyConfig{
    Store: provider.NewMemoryIdempotencyStore(),
    TTL:   5 * time.Minute,
})

registry.Register(idempotentProvider)

// 带幂等键发送
msg := provider.NewMessage("recipient@example.com").
    WithBody("重要消息").
    WithIdempotencyKey("unique-key-123")

// 相同键的第二次发送将返回缓存结果
result, _ := registry.Send(ctx, provider.ChannelEmail, msg)
```

### HTTP API Provider

```go
import provider "github.com/soulteary/provider-kit"

// 为外部短信 API 创建 HTTP Provider
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

### 验证码消息

```go
import provider "github.com/soulteary/provider-kit"

// 构建带语言支持的验证码消息
msg := provider.BuildVerificationMessage(
    "user@example.com",
    "123456",
    "zh-CN",
    provider.ChannelEmail,
)

// 发送验证邮件
result, err := registry.Send(ctx, provider.ChannelEmail, msg)
```

### Provider 工厂

```go
import provider "github.com/soulteary/provider-kit"

// 注册工厂
registry.RegisterFactory("smtp", provider.SMTPProviderFactory)

// 从配置 Map 创建 Provider
config := map[string]string{
    "host":     "smtp.example.com",
    "port":     "587",
    "username": "user",
    "password": "pass",
    "from":     "noreply@example.com",
}

smtpProvider, err := registry.CreateProvider("smtp", config)
```

## API 参考

### 通道类型

| 通道 | 描述 |
|------|------|
| `ChannelSMS` | 短信消息通道 |
| `ChannelEmail` | 邮件消息通道 |
| `ChannelHTTP` | 通用 HTTP API 通道 |

### Provider 接口

```go
type Provider interface {
    Send(ctx context.Context, msg *Message) (*SendResult, error)
    Channel() Channel
    Name() string
    Validate() error
}
```

### Message 方法

| 方法 | 描述 |
|------|------|
| `WithSubject(s)` | 设置消息主题（邮件） |
| `WithBody(s)` | 设置消息正文 |
| `WithTemplate(s)` | 设置模板名称 |
| `WithParams(map)` | 设置模板参数 |
| `WithCode(s)` | 设置验证码 |
| `WithLocale(s)` | 设置格式化语言 |
| `WithIdempotencyKey(s)` | 设置幂等键 |
| `AddMetadata(k, v)` | 添加元数据 |

### 错误原因

| 原因 | 描述 |
|------|------|
| `ReasonSendFailed` | 发送操作失败 |
| `ReasonProviderDown` | Provider 不可用 |
| `ReasonInvalidConfig` | 配置无效 |
| `ReasonRateLimited` | 被 Provider 限流 |
| `ReasonInvalidDestination` | 收件人无效 |
| `ReasonTimeout` | 请求超时 |
| `ReasonUnauthorized` | 认证失败 |
| `ReasonNotRegistered` | 未注册 Provider |
| `ReasonIdempotencyConflict` | 幂等冲突 |

### SMTP 配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `Host` | `string` | 必填 | SMTP 服务器主机 |
| `Port` | `int` | `587` | SMTP 服务器端口 |
| `Username` | `string` | `""` | 认证用户名 |
| `Password` | `string` | `""` | 认证密码 |
| `From` | `string` | 必填 | 发件人邮箱地址 |
| `FromName` | `string` | `""` | 发件人显示名称 |
| `UseTLS` | `bool` | `false` | 使用直接 TLS 连接 |
| `UseStartTLS` | `bool` | `true` | 使用 STARTTLS |
| `SkipTLSVerify` | `bool` | `false` | 跳过 TLS 验证 |
| `Timeout` | `time.Duration` | `30s` | 连接超时 |

### 重试配置

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `MaxRetries` | `int` | `3` | 最大重试次数 |
| `RetryDelay` | `time.Duration` | `100ms` | 初始重试延迟 |
| `MaxRetryDelay` | `time.Duration` | `5s` | 最大重试延迟 |
| `BackoffMultiplier` | `float64` | `2.0` | 指数退避乘数 |
| `RetryableReasons` | `[]ErrorReason` | `[ProviderDown, Timeout, RateLimited]` | 触发重试的错误 |

## 项目结构

```
provider-kit/
├── channel.go          # 通道类型定义
├── channel_test.go
├── errors.go           # 错误类型与归一化
├── errors_test.go
├── http.go             # HTTP API Provider
├── http_test.go
├── idempotency.go      # 幂等性支持
├── idempotency_test.go
├── interface.go        # Provider 接口定义
├── message.go          # Message 类型
├── message_test.go
├── registry.go         # Provider Registry
├── registry_test.go
├── result.go           # SendResult 类型
├── result_test.go
├── retry.go            # 重试逻辑
├── retry_test.go
├── smtp.go             # SMTP Provider
├── smtp_test.go
├── template.go         # 模板工具
├── template_test.go
├── go.mod
└── LICENSE
```

## 实现自定义 Provider

```go
type MyCustomProvider struct {
    config MyConfig
}

func (p *MyCustomProvider) Send(ctx context.Context, msg *provider.Message) (*provider.SendResult, error) {
    // 实现发送逻辑
    if err := p.doSend(msg); err != nil {
        return provider.NewFailureResult(p.Name(), p.Channel(), 
            provider.ErrSendFailed("发送失败", err)), err
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
        return provider.ErrInvalidConfig("API key 是必需的")
    }
    return nil
}
```

## 测试覆盖率

运行测试并查看覆盖率：

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## 环境要求

- Go 1.21 或更高版本

## 许可证

本项目采用 Apache License 2.0 许可证 - 详见 [LICENSE](LICENSE) 文件。

## 贡献

欢迎贡献！请随时提交 Pull Request。
