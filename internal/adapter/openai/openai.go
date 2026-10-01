// Package openai 实现 OpenAI 兼容上游协议适配器。
package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"llmgate/internal/adapter"
)

func init() {
	adapter.Register(&Adapter{})
}

// Adapter 直通 OpenAI 兼容协议：请求体原样转发。
type Adapter struct{}

// Name 返回适配器标识。
func (a *Adapter) Name() string { return "openai" }

// Do 发起非流式请求。
func (a *Adapter) Do(ctx context.Context, bc adapter.BaseChannel, apiKey string, req *adapter.Request) (*adapter.Response, error) {
	url := joinURL(bc.BaseURL, req.Path)
	var body io.Reader = strings.NewReader("")
	if req.Body != nil {
		body = req.Body
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, body)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}
	for k, v := range req.Headers {
		headers[k] = v
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	timeout := time.Duration(bc.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	outHeaders := map[string]string{}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		outHeaders["Content-Type"] = ct
	}
	return &adapter.Response{StatusCode: resp.StatusCode, Body: resp.Body, Headers: outHeaders}, nil
}

// DoStream 流式转发尚未实现（Phase 2）。
func (a *Adapter) DoStream(ctx context.Context, bc adapter.BaseChannel, apiKey string, req *adapter.Request) (io.ReadCloser, error) {
	return nil, errors.New("streaming not implemented yet")
}

// joinURL 拼接 base_url 与请求路径，避免重复的 /v1。
// 例：base="https://api.example.com/zen/go/v1"，path="/v1/models" → "https://api.example.com/zen/go/v1/models"；
//
//	base="https://api.deepseek.com"，path="/v1/models" → "https://api.deepseek.com/v1/models"。
func joinURL(base, p string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/v1") && strings.HasPrefix(p, "/v1") {
		p = strings.TrimPrefix(p, "/v1")
	}
	return b + p
}
