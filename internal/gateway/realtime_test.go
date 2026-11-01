package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/net/websocket"
)

func TestRealtimeURL(t *testing.T) {
	got, err := realtimeURL("https://api.example.com/v1", "up-model", url.Values{"model": {"alias"}, "foo": {"bar"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://api.example.com/v1/realtime?foo=bar&model=up-model"
	if got != want {
		t.Fatalf("realtimeURL = %q, want %q", got, want)
	}
}

func TestRealtimeProxyEcho(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			gotAuth = r.Header.Get("Authorization")
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			for {
				var msg string
				if err := websocket.Message.Receive(ws, &msg); err != nil {
					return
				}
				if err := websocket.Message.Send(ws, "echo:"+msg); err != nil {
					return
				}
			}
		},
	})
	defer upstream.Close()

	e := newTestEnv(t, upstream.URL)
	gw := httptest.NewServer(e.uh)
	defer gw.Close()

	wsURL := "ws" + gw.URL[len("http"):] + "/v1/realtime?model=m"
	cfg, err := websocket.NewConfig(wsURL, "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Header.Set("Authorization", "Bearer "+testToken)
	client, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()

	if err := websocket.Message.Send(client, "hello"); err != nil {
		t.Fatal(err)
	}
	var reply string
	if err := websocket.Message.Receive(client, &reply); err != nil {
		t.Fatal(err)
	}
	if reply != "echo:hello" {
		t.Fatalf("reply = %q, want echo:hello", reply)
	}
	if gotAuth != "Bearer sk-upstream" {
		t.Fatalf("upstream auth = %q", gotAuth)
	}
}
