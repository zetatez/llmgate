package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
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

// transcriptAggregator 按 final 片段累积转写，并对相邻片段做词级重叠去重。
type transcriptAggregator struct{ final string }

func (a *transcriptAggregator) apply(segment string, isFinal bool) string {
	if isFinal {
		a.final = joinTranscript(a.final, segment)
		return a.final
	}
	return joinTranscript(a.final, segment)
}

func joinTranscript(base, segment string) string {
	base, segment = strings.TrimSpace(base), strings.TrimSpace(segment)
	if base == "" {
		return segment
	}
	if segment == "" {
		return base
	}
	b, s := strings.Fields(base), strings.Fields(segment)
	max := len(b)
	if len(s) < max {
		max = len(s)
	}
	overlap := 0
	for n := max; n > 0; n-- {
		match := true
		for i := 0; i < n; i++ {
			if !strings.EqualFold(b[len(b)-n+i], s[i]) {
				match = false
				break
			}
		}
		if match {
			overlap = n
			break
		}
	}
	return strings.Join(append(b, s[overlap:]...), " ")
}

// proxyDeepgramAggregated 在透传 Deepgram 消息的同时附加 gateway_transcript（累积转写）。
func proxyDeepgramAggregated(client, upstream *websocket.Conn) {
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
		agg := &transcriptAggregator{}
		for {
			mt, msg, err := upstream.ReadMessage()
			if err != nil {
				finish()
				return
			}
			if mt == websocket.TextMessage {
				var m map[string]any
				if json.Unmarshal(msg, &m) == nil {
					if text, final := deepgramTranscript(m); text != "" {
						m["gateway_transcript"] = agg.apply(text, final)
						m["gateway_final"] = final
						if out, err := json.Marshal(m); err == nil {
							msg = out
						}
					}
				}
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

func deepgramTranscript(m map[string]any) (string, bool) {
	final, _ := m["is_final"].(bool)
	results, _ := m["results"].(map[string]any)
	channels, _ := results["channels"].([]any)
	if len(channels) == 0 {
		return "", final
	}
	ch, _ := channels[0].(map[string]any)
	alts, _ := ch["alternatives"].([]any)
	if len(alts) == 0 {
		return "", final
	}
	alt, _ := alts[0].(map[string]any)
	text, _ := alt["transcript"].(string)
	return strings.TrimSpace(text), final
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
