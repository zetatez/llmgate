// Package deepgram 实现 Deepgram 语音 API 的 HTTP 适配器（预录音频）。
// 实时流式转写走网关的 WebSocket /v1/listen 代理，不使用本适配器。
package deepgram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/outbound"
)

func init() {
	adapter.Register(&Adapter{})
}

// Adapter 直通 Deepgram REST API：Authorization 使用 Token 方案。
type Adapter struct{}

func (a *Adapter) Name() string { return "deepgram" }

func (a *Adapter) Do(ctx context.Context, bc adapter.BaseChannel, apiKey string, req *adapter.Request) (*adapter.Response, error) {
	base := strings.TrimRight(bc.BaseURL, "/")
	path := req.Path
	if path == "" {
		path = "/v1/listen"
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	endpoint := base + path

	var body io.Reader = strings.NewReader("")
	if req.Body != nil {
		body = req.Body
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, endpoint, body)
	if err != nil {
		return nil, err
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Authorization", "Token "+apiKey)
	httpReq.Header.Set("Content-Type", contentType)

	timeout := time.Duration(bc.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := &http.Client{Transport: outbound.Transport(), Timeout: timeout}
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

// DoStream Deepgram 的流式接口是 WebSocket /v1/listen，由 gateway 单独代理。
func (a *Adapter) DoStream(context.Context, adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
	return nil, fmt.Errorf("deepgram streaming uses WebSocket /v1/listen")
}
