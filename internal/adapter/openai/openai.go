// Package openai 实现 OpenAI 兼容上游协议适配器。
package openai

import (
	"context"
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

// DoStream 发起流式请求，返回 SSE 数据流；非 2xx 返回 *StatusError。
func (a *Adapter) DoStream(ctx context.Context, bc adapter.BaseChannel, apiKey string, req *adapter.Request) (io.ReadCloser, error) {
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
		"Accept":        "text/event-stream",
	}
	for k, v := range req.Headers {
		headers[k] = v
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	timeout := time.Duration(bc.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		headers := map[string]string{}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			headers["Content-Type"] = ct
		}
		return nil, &adapter.StatusError{StatusCode: resp.StatusCode, Body: b, HeaderMap: headers}
	}
	return resp.Body, nil
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
