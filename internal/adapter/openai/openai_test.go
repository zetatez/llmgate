package openai

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"llmgate/internal/adapter"
)

// resetGlobals 清空包级代理状态，避免测试间相互污染。
func resetGlobals() {
	httpProxyURL = nil
	socksDialer = nil
}

func TestSetProxy(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	// 空 = 直连，返回 nil。
	if err := SetProxy(""); err != nil {
		t.Fatalf("SetProxy(\"\") = %v, want nil", err)
	}
	if httpProxyURL != nil || socksDialer != nil {
		t.Fatalf("empty proxy should leave globals nil")
	}

	// 无效 URL。
	if err := SetProxy("://bad"); err == nil {
		t.Fatalf("SetProxy(invalid) = nil, want error")
	}

	// http 代理。
	if err := SetProxy("http://proxy.example:8080"); err != nil {
		t.Fatalf("SetProxy(http) = %v, want nil", err)
	}
	if httpProxyURL == nil || socksDialer != nil {
		t.Fatalf("http proxy should set httpProxyURL only")
	}

	// https 代理。
	resetGlobals()
	if err := SetProxy("https://proxy.example:8443"); err != nil {
		t.Fatalf("SetProxy(https) = %v, want nil", err)
	}
	if httpProxyURL == nil {
		t.Fatalf("https proxy should set httpProxyURL")
	}

	// socks5 无认证。
	resetGlobals()
	if err := SetProxy("socks5://host.example:1080"); err != nil {
		t.Fatalf("SetProxy(socks5) = %v, want nil", err)
	}
	if socksDialer == nil {
		t.Fatalf("socks5 proxy should set socksDialer")
	}

	// socks5h 带认证。
	resetGlobals()
	if err := SetProxy("socks5h://user:pass@host.example:1080"); err != nil {
		t.Fatalf("SetProxy(socks5h auth) = %v, want nil", err)
	}

	// 不支持的 scheme。
	if err := SetProxy("ftp://host"); err == nil {
		t.Fatalf("SetProxy(ftp) = nil, want error")
	}
}

func TestJoinURL(t *testing.T) {
	cases := []struct {
		base, p, want string
	}{
		{"https://api.example.com/zen/go/v1", "/v1/models", "https://api.example.com/zen/go/v1/models"},
		{"https://api.deepseek.com", "/v1/models", "https://api.deepseek.com/v1/models"},
		{"https://api.deepseek.com/", "/v1/models", "https://api.deepseek.com/v1/models"},
		{"https://api.example.com/v1x", "/v1/models", "https://api.example.com/v1x/v1/models"}, // 不以 /v1 结尾
		{"https://api.example.com/v1", "/models", "https://api.example.com/v1/models"},
		{"base", "path", "basepath"},
	}
	for _, c := range cases {
		if got := joinURL(c.base, c.p); got != c.want {
			t.Errorf("joinURL(%q, %q) = %q, want %q", c.base, c.p, got, c.want)
		}
	}
}

func TestName(t *testing.T) {
	var a Adapter
	if a.Name() != "openai" {
		t.Fatalf("Name() = %q, want \"openai\"", a.Name())
	}
	// 通过注册表取回同一个适配器。
	if got := adapter.Get("openai"); got == nil {
		t.Fatalf("adapter.Get(\"openai\") = nil")
	}
}

func TestNewClientDefaultTimeout(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	c := newClient(0) // timeout<=0 → 默认
	if c.Timeout != 60*time.Second {
		t.Fatalf("newClient(0).Timeout = %v, want 60s", c.Timeout)
	}

	c = newClient(10 * time.Second)
	if c.Timeout != 10*time.Second {
		t.Fatalf("newClient(10s).Timeout = %v, want 10s", c.Timeout)
	}

	// 配置 http 代理分支。
	if err := SetProxy("http://proxy.example:8080"); err != nil {
		t.Fatal(err)
	}
	c = newClient(time.Second)
	if _, ok := c.Transport.(*http.Transport); !ok {
		t.Fatalf("http proxy client should use *http.Transport")
	}

	// 配置 socks 分支。
	resetGlobals()
	SetProxy("socks5://host.example:1080")
	c = newClient(time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatalf("socks client should have DialContext set")
	}
}

func TestNewStreamClient(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	c := newStreamClient(0)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout != 60*time.Second {
		t.Fatalf("newStreamClient(0) default header timeout wrong")
	}
	if c.Timeout != 0 {
		t.Fatalf("stream client must not set total timeout")
	}

	// socks 分支。
	SetProxy("socks5://host.example:1080")
	c = newStreamClient(time.Second)
	tr, ok = c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil || tr.ResponseHeaderTimeout != time.Second {
		t.Fatalf("socks stream client config wrong")
	}

	// http 分支。
	resetGlobals()
	SetProxy("http://proxy.example:8080")
	c = newStreamClient(time.Second)
	tr, ok = c.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatalf("http proxy stream client config wrong")
	}
}

// countingReader 记录 Close 被调用的次数，用于验证 Close 副作用传播。
type countingReader struct {
	rc      io.ReadCloser
	closedN int
}

func (c *countingReader) Read(p []byte) (int, error) { return c.rc.Read(p) }
func (c *countingReader) Close() error               { c.closedN++; return c.rc.Close() }

func TestIdleReadCloser(t *testing.T) {
	// 默认超时分支：d<=0。
	inner := &countingReader{rc: io.NopCloser(strings.NewReader("abc"))}
	ir := newIdleReadCloser(inner, 0).(*idleReadCloser)
	if ir.d != 60*time.Second {
		t.Fatalf("default idle duration = %v, want 60s", ir.d)
	}
	buf := make([]byte, 3)
	n, err := ir.Read(buf)
	if n != 3 || err != nil {
		t.Fatalf("Read = %d,%v, want 3,nil", n, err)
	}
	if err := ir.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if inner.closedN != 1 {
		t.Fatalf("underlying Close not propagated (%d)", inner.closedN)
	}
	if ir.timer != nil {
		t.Fatalf("after Close timer should be nil")
	}
}

func TestIdleReadCloserTimeout(t *testing.T) {
	// 模拟一个会永久阻塞的底层 reader。空闲超时后将关闭底层连接，Read 返回错误。
	timeout := 50 * time.Millisecond
	blocking := newBlockingReadCloser()
	ir := newIdleReadCloser(blocking, timeout).(*idleReadCloser)
	start := time.Now()
	buf := make([]byte, 4)
	_, err := ir.Read(buf)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("Read over idle timeout should return error, got nil")
	}
	if !blocking.isClosed() {
		t.Fatalf("blocking reader should have been closed by timer")
	}
	if elapsed < timeout {
		t.Fatalf("Read returned before idle timeout: %v", elapsed)
	}

	// 第二次 Read：timer 已经 fire（Stop 返回 false），应走 drain 分支。
	ir2 := newIdleReadCloser(newBlockingReadCloser(), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond) // 等 fire
	_, _ = ir2.Read(make([]byte, 1))
	_ = ir2.Close()
}

// blockingReadCloser 的 Read 永久阻塞，直到 Close 解除（channel 驱动，竞争安全）。
type blockingReadCloser struct {
	done chan struct{}
	once sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{done: make(chan struct{})}
}

func (b *blockingReadCloser) Read(p []byte) (int, error) {
	<-b.done
	return 0, errors.New("connection closed by idle timeout")
}

func (b *blockingReadCloser) Close() error {
	b.once.Do(func() { close(b.done) })
	return nil
}

func (b *blockingReadCloser) isClosed() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

func TestDo(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-123" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("X-Custom") != "cv" {
			t.Errorf("X-Custom = %q", r.Header.Get("X-Custom"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"a":1}` {
			t.Errorf("body = %q", body)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := &Adapter{}
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 5000}
	req := &adapter.Request{
		Method:  "POST",
		Path:    "/v1/models",
		Body:    strings.NewReader(`{"a":1}`),
		Headers: map[string]string{"X-Custom": "cv", "Content-Type": "text/override"},
	}
	resp, err := a.Do(context.Background(), bc, "key-123", req)
	if err != nil {
		t.Fatalf("Do = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d", resp.StatusCode)
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("Content-Type header = %q", resp.Headers["Content-Type"])
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != "ok" {
		t.Fatalf("body = %q", got)
	}
}

// socks5TestProxy 是一个极简 SOCKS5 代理服务器，握手成功后把连接当 HTTP 服务器使用，
// 用于真正走到 newClient/newStreamClient 的 socksDialer.Dial 分支。
func socks5TestProxy(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	// 判断 CONNECT 请求的目的地址类型并读取地址与端口。
	readAddr := func(r *bufio.Reader) (string, error) {
		atyp, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		switch atyp {
		case 0x01: // IPv4
			b := make([]byte, 4)
			if _, err := io.ReadFull(r, b); err != nil {
				return "", err
			}
			return net.IP(b).String(), nil
		case 0x03: // 域名
			n, err := r.ReadByte()
			if err != nil {
				return "", err
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return "", err
			}
			return string(b), nil
		case 0x04: // IPv6
			b := make([]byte, 16)
			if _, err := io.ReadFull(r, b); err != nil {
				return "", err
			}
			return net.IP(b).String(), nil
		}
		return "", errors.New("unknown address type")
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				if _, err := r.ReadByte(); err != nil { // ver
					return
				}
				n, _ := r.ReadByte() // 认证方法数
				io.CopyN(io.Discard, r, int64(n))
				c.Write([]byte{0x05, 0x00}) // 无认证
				// CONNECT 请求头。
				if b, _ := r.ReadByte(); b != 0x05 {
					return
				}
				r.ReadByte() // cmd
				r.ReadByte() // rsv
				if _, err := readAddr(r); err != nil {
					return
				}
				io.CopyN(io.Discard, r, 2) // 端口
				// 成功后直接以 HTTP 服务器身份回复。
				c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
				handler(&httpResponseWriter{c: c}, &http.Request{Method: "GET"})
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// httpResponseWriter 让测试 HTTP handler 能通过现有的 socks 隧道连接写回响应。
type httpResponseWriter struct {
	c     net.Conn
	wrote bool
	code  int
}

func (w *httpResponseWriter) Header() http.Header { return http.Header{} }
func (w *httpResponseWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.code = code
	fmt.Fprintf(w.c, "HTTP/1.1 %d OK\r\nContent-Length: 2\r\n\r\n", code)
}
func (w *httpResponseWriter) Write(b []byte) (int, error) {
	w.WriteHeader(200)
	return w.c.Write(b)
}
func (w *httpResponseWriter) Flush() {}

func TestSetProxySocksDial(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	srv := socks5TestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// 空 host 会创建 dialer（x/net/proxy.SOCKS5 从不返回 err），仅验证配置成功。
	if err := SetProxy("socks5://" + srv); err != nil {
		t.Fatalf("SetProxy(socks5) = %v", err)
	}

	// 非流式：走到 newClient 的 socksDialer.DialContext。
	bc := adapter.BaseChannel{BaseURL: "http://upstream.example", TimeoutMS: 8000}
	req := &adapter.Request{Method: "GET", Path: "/v1/models"}
	resp, err := (&Adapter{}).Do(context.Background(), bc, "k", req)
	if err != nil {
		t.Fatalf("Do via socks = %v", err)
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(rb) != "ok" {
		t.Fatalf("socks body = %q", rb)
	}

	// 流式：走到 newStreamClient 的 socksDialer.DialContext。
	rc, err := (&Adapter{}).DoStream(context.Background(), bc, "k", req)
	if err != nil {
		t.Fatalf("DoStream via socks = %v", err)
	}
	rc.Close()
}

func TestDoNewRequestErrorAndTimeoutDefault(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	a := &Adapter{}
	// 非法 method 触发 http.NewRequestWithContext 错误。
	bad := &adapter.Request{Method: "GE\x00T", Path: "/v1/models"}
	if _, err := a.Do(context.Background(), adapter.BaseChannel{BaseURL: "http://x"}, "k", bad); err == nil {
		t.Fatalf("Do(bad method) = nil, want error")
	}
	// DoStream 同样。
	if _, err := a.DoStream(context.Background(), adapter.BaseChannel{BaseURL: "http://x"}, "k", bad); err == nil {
		t.Fatalf("DoStream(bad method) = nil, want error")
	}

	// TimeoutMS=0 → 默认 60s / 2min 分支（无代理直连，立刻返回）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("z"))
	}))
	defer srv.Close()
	resp, err := a.Do(context.Background(), adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 0}, "k",
		&adapter.Request{Method: "GET", Path: "/v1/models"})
	if err != nil {
		t.Fatalf("Do(TimeoutMS=0) = %v", err)
	}
	resp.Body.Close()

	// DoStream 覆盖 TimeoutMS=0（2min 默认） 与 req.Headers 循环。
	rc, err := a.DoStream(context.Background(), adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 0}, "k",
		&adapter.Request{Method: "GET", Path: "/v1/models", Headers: map[string]string{"X-A": "1"}})
	if err != nil {
		t.Fatalf("DoStream(TimeoutMS=0) = %v", err)
	}
	rc.Close()
}

func TestIdleReadCloserTimerFiredDrain(t *testing.T) {
	blk := newBlockingReadCloser()
	ir := newIdleReadCloser(blk, time.Hour).(*idleReadCloser)
	// 手动把定时器触发到已 fire 状态。
	ir.mu.Lock()
	ir.timer.Reset(time.Nanosecond)
	ir.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for !blk.isClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !blk.isClosed() {
		t.Fatal("timer did not fire")
	}
	// 此时 Stop() 返回 false，Read 内走通道 drain 分支。
	_, _ = ir.Read(make([]byte, 1))
}

func TestDoNilBodyAndCtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("hi"))
	}))
	defer srv.Close()

	a := &Adapter{}
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 5000}
	// req.Body == nil → 用空 body。
	req := &adapter.Request{Method: "GET", Path: "/v1/models"}
	if _, err := a.Do(context.Background(), bc, "k", req); err != nil {
		t.Fatalf("Do(nil body) = %v", err)
	}

	// 已取消的 ctx → client.Do 报错分支。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Do(ctx, bc, "k", req); err == nil {
		t.Fatalf("Do(cancelled ctx) = nil, want error")
	}

	// DoStream 同样覆盖取消分支。
	if _, err := a.DoStream(ctx, bc, "k", req); err == nil {
		t.Fatalf("DoStream(cancelled ctx) = nil, want error")
	}
}

func TestDoNon2xxPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	a := &Adapter{}
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 5000}
	resp, err := a.Do(context.Background(), bc, "k", &adapter.Request{Method: "GET", Path: "/v1/models"})
	if err != nil {
		t.Fatalf("Do(non-2xx) = %v", err)
	}
	// 非流式非 2xx 透传，不报错。
	if resp.StatusCode != 429 {
		t.Fatalf("StatusCode = %d, want 429", resp.StatusCode)
	}
}

func TestDoStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		fl, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		// SSE 流分段写入，验证合并读取。
		w.Write([]byte("data: hello\n\n"))
		fl.Flush()
		w.Write([]byte("data: "))
		fl.Flush()
		w.Write([]byte("world\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	a := &Adapter{}
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 5000}
	rc, err := a.DoStream(context.Background(), bc, "k", &adapter.Request{Method: "POST", Path: "/v1/chat/completions", Body: strings.NewReader(`{}`)})
	if err != nil {
		t.Fatalf("DoStream = %v", err)
	}
	defer rc.Close()
	all, _ := io.ReadAll(rc)
	want := "data: hello\n\ndata: world\n\n"
	if string(all) != want {
		t.Fatalf("stream = %q, want %q", all, want)
	}
}

func TestDoStreamNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	a := &Adapter{}
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 5000}
	rc, err := a.DoStream(context.Background(), bc, "k", &adapter.Request{Method: "POST", Path: "/v1/chat/completions"})
	if rc != nil {
		t.Fatalf("DoStream non-2xx should return nil ReadCloser")
	}
	var se *adapter.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("DoStream non-2xx err = %v, want *StatusError", err)
	}
	if se.StatusCode != 400 {
		t.Fatalf("StatusCode = %d, want 400", se.StatusCode)
	}
	if se.HeaderMap["Content-Type"] != "application/json" {
		t.Fatalf("HeaderMap = %v", se.HeaderMap)
	}
	if !strings.Contains(string(se.Body), "bad") {
		t.Fatalf("Body = %q", se.Body)
	}
}

func TestDoStreamIdleTimeoutTriggers(t *testing.T) {
	// 服务端发送响应头后挂起（零字节流），idleReadCloser 应关闭连接并返回错误。
	release := make(chan struct{})
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		w.WriteHeader(200)
		fl.Flush()
		close(started)
		<-release
	}))
	defer srv.Close()

	a := &Adapter{}
	// 短超时让空闲检测快速触发（客户端 header 超时 + 空闲超时都取该值）。
	bc := adapter.BaseChannel{BaseURL: srv.URL, TimeoutMS: 100}
	rc, err := a.DoStream(context.Background(), bc, "k", &adapter.Request{Method: "GET", Path: "/v1/chat/completions"})
	if err != nil {
		close(release)
		t.Fatalf("DoStream = %v", err)
	}
	<-started
	buf := make([]byte, 16)
	start := time.Now()
	n, err := rc.Read(buf)
	elapsed := time.Since(start)
	close(release)
	rc.Close()
	if n != 0 || err == nil {
		t.Fatalf("Read over idle timeout = (%d,%v), want (0,error)", n, err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("idle Read returned too early: %v", elapsed)
	}
}
