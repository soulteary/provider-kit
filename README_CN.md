# provider-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/provider-kit.svg)](https://pkg.go.dev/github.com/soulteary/provider-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/provider-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/provider-kit)

[English](README.md)

一个轻量级、可扩展的 Go 消息发送库，支持邮件、短信、钉钉等通道，通过插件化 Provider 实现，内置重试、幂等性与错误归一化。

## 功能特性

- **Provider 接口** - 清晰的接口定义，便于实现自定义消息提供者
- **Registry 模式** - 基于通道类型的集中式 Provider 管理
- **内置 Provider** - SMTP 邮件发送、HTTP API 外部服务（短信、钉钉等）
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

store := provider.NewMemoryIdempotencyStore()
defer store.Close() // 停掉清理 goroutine

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

用同一个键第二次发送会返回已记录的结果，而不是再发一次。
`WrapWithIdempotency(provider, store, ttl)` 是简写形式。

#### 并发

两个调用方可能同时带着同一个幂等键进来，所以"占位"必须是原子的。实现了 `Reserver`
的存储就具备这个能力：

```go
type Reserver interface {
    // Reserve 原子地占下 key，并在同一次操作中返回任何已记录的结果。
    // token 标识这一次占位。
    Reserve(ctx context.Context, key string, ttl time.Duration) (string, *SendResult, error)
    // Finalize 把结果记录到某次占位上。
    Finalize(ctx context.Context, key, token string, result *SendResult, ttl time.Duration) error
    // Abandon 释放一次永远不会产出结果的占位。
    Abandon(ctx context.Context, key, token string) error
}
```

`MemoryIdempotencyStore` 实现了它。对 Redis 来说，`Reserve` 就是
`SET key value NX PX ttl`。

只实现了 `IdempotencyStore` 的存储仍然可用，走的是"先查后做"的路径——`Get`、发送、
`Set`。**那条路径在并发下不成立**：两个带同一个键的请求都查不到，都调用 provider，
于是发出两条消息。这个窗口和 provider 调用本身一样宽。

请处理并发情形：

```go
result, err := registry.Send(ctx, provider.ChannelEmail, msg)
switch {
case errors.Is(err, provider.ErrSendInFlight):
    // 另一个调用方持有占位，且还没记录结果。
    // 稍后重试；不要再发第二条消息。
case errors.Is(err, provider.ErrClaimSuperseded):
    // 这次占位被接管了。请重新读取结果，而不是重发。
case err != nil:
    return err
}
```

#### 什么会上报、什么不会

- **发送之前的存储读取失败会被返回。** 在那个时刻还什么都没发出去，所以拒绝是安全的，
  你可以重试。此前存储错误被当作缓存未命中，于是存储不可用时幂等性会静默关闭、消息
  再发一次，而且什么都不上报。
- **记录结果失败是故意被吞掉的。** 消息已经发出去了；在那里报错会把"遇错就重试"的调用
  方推向再发一次。代价是后续某次重试没有被去重，这严格优于把一次已成功的发送变成重复
  发送。
- **发送失败且没有产出结果时会放弃占位**，这样重试不会被整个 TTL 挡住。

#### 内存存储的注意事项

`MemoryIdempotencyStore` **只在单个进程内**去重。负载均衡后面的多个实例各自持有自己的
map，于是同一个键可能每个实例各发一次——多实例部署请使用共享存储（Redis）。

用完存储请调用 `Close()`。它会停掉清理 goroutine，否则该 goroutine 会在进程的整个生命
周期里一直运行，并让存储及其所有条目保持可达。

`Get` 和 `Reserve` 返回的是存储结果的**副本**，因此一个调用方修改拿到的对象不会改变其他
调用方看到的内容。

### HTTP API Provider

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
| `ChannelHTTP` | 通用 HTTP API 通道（外部短信、钉钉等） |
| `ChannelDingTalk` | 钉钉工作通知通道（通过 herald-dingtalk HTTP 服务） |

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

## 升级说明（v1.7.0）

新增一个接口、两个哨兵错误和一个方法，没有删除任何东西。幂等发送在一些此前会发送的情形
下改为拒绝。

- **存储读取失败现在会让发送失败。** 此前的判断是
  `if result, found, err := p.store.Get(...); err == nil && found`，于是存储错误与缓存
  未命中无法区分：存储不可用时，**幂等性静默关闭、消息被再发一次**，而且什么都不上报。
  那次读取失败时还什么都没发出去，所以拒绝是安全的——请处理这个错误并重试。
- **并发调用方会得到 `ErrSendInFlight`，而不是再发一次。**
  `IdempotentProvider.Send` 此前是"先查后做"，于是两个带同一个键的请求都查不到、都调用
  provider，发出两条消息——正是幂等性要防的那件事。**请为任何可能并发带上同一个键的调用
  方加上 `ErrSendInFlight` 和 `ErrClaimSuperseded` 的处理。**
- **新增 `Reserver`。** 实现它的存储会原子地占下键，并在同一次操作中返回已记录的结果。
  `MemoryIdempotencyStore` 已实现。只实现 `IdempotencyStore` 的存储保留旧的"先查后做"
  路径，它在并发下不成立——如果你写了自定义存储，实现 `Reserver` 才能让它变安全。
- **新增 `MemoryIdempotencyStore.Close()`，请记得调用。** 该存储此前启动了一个无法停止
  的清理 goroutine，于是**每个创建过的存储都会泄漏一个 goroutine**，而这个 goroutine
  还让存储及其条目保持可达。
- **`Get` 和 `Reserve` 返回副本。** `Get` 此前直接返回存储的 `*SendResult`，被每个命中
  该键的调用方共享，于是一个调用方修改它就改变了其他调用方看到的内容。
- **发送失败会放弃占位**，这样重试不会被整个 TTL 挡住。
- **环境要求里写的是 Go 1.26**；`go.mod` 需要 `1.27.0`。

有意保持不变：记录结果失败仍然被吞掉，因为消息已经发出去了，在那里暴露错误会让
"遇错就重试"的调用方把它发两次。

## 环境要求

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）

## 许可证

本项目采用 Apache License 2.0 许可证 - 详见 [LICENSE](LICENSE) 文件。

## 贡献

欢迎贡献！请随时提交 Pull Request。
