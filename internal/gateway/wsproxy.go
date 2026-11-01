package gateway

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"llmgate/internal/outbound"
)

// newWSDialer 构造带 LGM_PROXY 支持的 WebSocket 客户端。
func newWSDialer() *websocket.Dialer {
	d := &websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	if u := outbound.HTTPProxy(); u != nil {
		d.Proxy = http.ProxyURL(u)
	}
	d.NetDialContext = outbound.DialContext
	return d
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin:      func(*http.Request) bool { return true }, // 已通过 Bearer 鉴权
	HandshakeTimeout: 10 * time.Second,
}

// proxyWS 双向转发 WebSocket 消息（保留 text/binary 帧类型），任一侧断开即关闭双方。
func proxyWS(client, upstream *websocket.Conn) {
	done := make(chan struct{})
	var once sync.Once
	finish := func() {
		once.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
			close(done)
		})
	}
	go func() {
		for {
			mt, msg, err := upstream.ReadMessage()
			if err != nil {
				finish()
				return
			}
			if err := client.WriteMessage(mt, msg); err != nil {
				finish()
				return
			}
		}
	}()
	go func() {
		for {
			mt, msg, err := client.ReadMessage()
			if err != nil {
				finish()
				return
			}
			if err := upstream.WriteMessage(mt, msg); err != nil {
				finish()
				return
			}
		}
	}()
	<-done
}
