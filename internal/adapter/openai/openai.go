// Package openai 实现 OpenAI 兼容上游协议适配器。
package openai

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"llmgate/internal/adapter"
)

func init() {
	adapter.Register(&Adapter{})
}

// httpProxyURL 已解析的代理地址（http/https 用）。
var httpProxyURL *url.URL

// socksDialer SOCKS5 拨号器（socks5/socks5h 用）。
var socksDialer proxy.Dialer

// SetProxy 配置上游出站代理，LGM_PROXY 提供（socks5:// / socks5h:// / http:// / https://）。空=直连。
func SetProxy(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid LGM_PROXY: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		httpProxyURL = u
		return nil
	case "socks5", "socks5h":
		// socks5h：域名交给代理解析（规避本地 DNS 污染）
		auth := &proxy.Auth{}
		if u.User != nil {
			auth.User = u.User.Username()
			auth.Password, _ = u.User.Password()
		} else {
			auth = nil
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			return fmt.Errorf("invalid socks5 proxy: %w", err)
		}
		socksDialer = d
		return nil
	default:
		return fmt.Errorf("unsupported LGM_PROXY scheme: %s (支持 http/https/socks5/socks5h)", u.Scheme)
	}
}

// newClient 构造 httpClient；配置了代理时走代理。
func newClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if socksDialer != nil {
		tr := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return socksDialer.Dial(network, addr)
			},
		}
		return &http.Client{Transport: tr, Timeout: timeout}
	}
	if httpProxyURL != nil {
		tr := &http.Transport{Proxy: http.ProxyURL(httpProxyURL)}
		return &http.Client{Transport: tr, Timeout: timeout}
	}
	return &http.Client{Timeout: timeout}
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
	client := newClient(timeout)
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
	client := newClient(timeout)
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
