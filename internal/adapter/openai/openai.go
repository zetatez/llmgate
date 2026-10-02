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
	"sync"
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

// newClient 构造 httpClient；配置了代理时走代理。带总超时（含响应体读取），用于非流式请求。
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

// newStreamClient 构造流式 httpClient：只限制"响应头首字节"耗时，不设总超时，
// 避免长流式会话被 Client.Timeout 在整个响应体读取中计时而误杀。
func newStreamClient(headerTimeout time.Duration) *http.Client {
	if headerTimeout <= 0 {
		headerTimeout = 60 * time.Second
	}
	tr := &http.Transport{ResponseHeaderTimeout: headerTimeout}
	if socksDialer != nil {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		}
	}
	if httpProxyURL != nil {
		tr.Proxy = http.ProxyURL(httpProxyURL)
	}
	return &http.Client{Transport: tr} // 无总超时：长流依赖 ctx 取消 + 响应体空闲超时
}

// idleReadCloser 在响应体上叠加"空闲超时"：超过 d 无任何字节读到则关闭底层连接。
// 用于流式：既不设总超时（长流不断），又能让真正卡住（零字节）的流安全终止并触发网关错误事件。
type idleReadCloser struct {
	rc    io.ReadCloser
	d     time.Duration
	timer *time.Timer
	mu    sync.Mutex
}

func newIdleReadCloser(rc io.ReadCloser, d time.Duration) io.ReadCloser {
	if d <= 0 {
		d = 60 * time.Second
	}
	i := &idleReadCloser{rc: rc, d: d}
	i.timer = time.AfterFunc(d, func() {
		i.mu.Lock()
		rc.Close() // 关闭底层连接，解除阻塞中的 Read
		i.mu.Unlock()
	})
	return i
}

func (i *idleReadCloser) Read(p []byte) (int, error) {
	n, err := i.rc.Read(p)
	i.mu.Lock()
	if i.timer != nil {
		if !i.timer.Stop() {
			select {
			case <-i.timer.C:
			default:
			}
		}
		i.timer.Reset(i.d)
	}
	i.mu.Unlock()
	return n, err
}

func (i *idleReadCloser) Close() error {
	i.mu.Lock()
	if i.timer != nil {
		i.timer.Stop()
		i.timer = nil
	}
	i.mu.Unlock()
	return i.rc.Close()
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
	// 组装头：客户端透传/渠道配置。强制保证鉴权头与 JSON 内容类型，
	// 防止渠道 extra_headers 或透传头覆盖网关的真实 Key/类型。
	headers := map[string]string{"Content-Type": "application/json"}
	for k, v := range req.Headers {
		headers[k] = v
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

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
	headers := map[string]string{"Content-Type": "application/json", "Accept": "text/event-stream"}
	for k, v := range req.Headers {
		headers[k] = v
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	// 鉴权头/类型强制，防止渠道配置或透传头覆盖
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// 流式：只限响应头耗时，不设总超时；响应体叠加空闲超时（长流不断、真卡能自动断）
	timeout := time.Duration(bc.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	client := newStreamClient(timeout)
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
	return newIdleReadCloser(resp.Body, timeout), nil
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
