// Package adapter 定义上游协议适配层接口，便于后续扩展非 OpenAI 兼容协议。
package adapter

import (
	"context"
	"fmt"
	"io"
)

// Request 是路由层交给适配器的统一转发描述。
// 网关侧按 OpenAI 协议反序列化后，映射到各上游的实际请求。
type Request struct {
	Method  string            // HTTP 方法
	Path    string            // 上游相对路径，如 /v1/chat/completions
	Body    io.Reader         // 请求体（已按上游适配转换）
	Headers map[string]string // 需要附加/覆盖的头
	Model   string            // 本次请求的对外模型名（仅日志用）
}

// StatusError 是上游返回非 2xx 时的错误，携带状态码与响应体，供路由层判定 failover 或透传。
type StatusError struct {
	StatusCode int
	Body       []byte
	HeaderMap  map[string]string
}

// Error 实现 error 接口。
func (e *StatusError) Error() string { return fmt.Sprintf("upstream status %d", e.StatusCode) }

// Response 返回给路由层的上游响应。
type Response struct {
	StatusCode int
	Body       io.ReadCloser
	Headers    map[string]string
}

// Adapter 将一条 OpenAI 兼容请求转换为上游协议并执行。
type Adapter interface {
	// Name 适配器标识，如 "openai"。
	Name() string
	// Do 发起请求（非流式），ctx 控制取消与超时。
	Do(ctx context.Context, channel BaseChannel, key string, req *Request) (*Response, error)
	// DoStream 发起流式请求，返回可读取的 SSE 流（或等价流）。
	DoStream(ctx context.Context, channel BaseChannel, key string, req *Request) (io.ReadCloser, error)
}

// BaseChannel 是适配器所需的最小渠道信息（从 channels 表映射而来）。
type BaseChannel struct {
	BaseURL   string
	TimeoutMS int
}

// Registry 按名称保存已注册适配器。
var Registry = map[string]Adapter{}

// Register 注册适配器到全局注册表。
func Register(a Adapter) { Registry[a.Name()] = a }

// Get 按名称取适配器，不存在返回 nil。
func Get(name string) Adapter { return Registry[name] }
