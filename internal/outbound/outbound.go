// Package outbound 统一管理上游出站代理（HTTP/SOCKS5），供 HTTP 与 WebSocket 共用。
package outbound

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

var (
	httpProxyURL *url.URL
	socksDialer  proxy.Dialer
)

// SetProxy 配置上游出站代理：socks5:// / socks5h:// / http:// / https://；空=直连。
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
		var auth *proxy.Auth
		if u.User != nil {
			auth = &proxy.Auth{User: u.User.Username()}
			auth.Password, _ = u.User.Password()
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			return fmt.Errorf("invalid socks5 proxy: %w", err)
		}
		socksDialer = d
		return nil
	default:
		return fmt.Errorf("unsupported LGM_PROXY scheme: %s (supported: http/https/socks5/socks5h)", u.Scheme)
	}
}

// HTTPProxy 返回 HTTP/HTTPS 代理地址（无则 nil）。
func HTTPProxy() *url.URL { return httpProxyURL }

// DialContext 通过 SOCKS5 或直连建立 TCP 连接。
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if socksDialer != nil {
		return socksDialer.Dial(network, addr)
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, network, addr)
}

// ResetForTest 清空代理状态（仅测试使用）。
func ResetForTest() {
	httpProxyURL = nil
	socksDialer = nil
}

// HTTPProxyForTest 返回当前 HTTP 代理（仅测试使用）。
func HTTPProxyForTest() *url.URL { return httpProxyURL }

// SOCKSForTest 返回当前 SOCKS 拨号器（仅测试使用）。
func SOCKSForTest() proxy.Dialer { return socksDialer }

// Transport 返回带代理配置的 HTTP Transport。
func Transport() *http.Transport {
	tr := &http.Transport{DialContext: DialContext}
	if httpProxyURL != nil {
		tr.Proxy = http.ProxyURL(httpProxyURL)
	}
	return tr
}
