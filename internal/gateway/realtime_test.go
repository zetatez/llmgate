package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
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

func wsEchoUpstream(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	gotAuth := new(string)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		*gotAuth = r.Header.Get("Authorization")
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	return srv, gotAuth
}

func TestRealtimeProxyEcho(t *testing.T) {
	upstream, gotAuth := wsEchoUpstream(t)
	defer upstream.Close()

	e := newTestEnv(t, upstream.URL)
	gw := httptest.NewServer(e.uh)
	defer gw.Close()

	wsURL := "ws" + gw.URL[len("http"):] + "/v1/realtime?model=m"
	h := http.Header{"Authorization": {"Bearer " + testToken}}
	client, _, err := websocket.DefaultDialer.Dial(wsURL, h)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()

	if err := client.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, reply, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(reply) != "hello" {
		t.Fatalf("reply = %q, want hello", reply)
	}
	if *gotAuth != "Bearer sk-upstream" {
		t.Fatalf("upstream auth = %q", *gotAuth)
	}
}

func TestDeepgramListenProxy(t *testing.T) {
	upstream, gotAuth := wsEchoUpstream(t)
	defer upstream.Close()

	e := newTestEnv(t, upstream.URL)
	if _, err := e.db.Exec(`UPDATE channels SET adapter='deepgram'`); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(e.uh)
	defer gw.Close()

	wsURL := "ws" + gw.URL[len("http"):] + "/v1/listen?model=m&language=en"
	h := http.Header{"Authorization": {"Token " + testToken}}
	client, _, err := websocket.DefaultDialer.Dial(wsURL, h)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()

	audio := []byte{0x01, 0x02, 0x03}
	if err := client.WriteMessage(websocket.BinaryMessage, audio); err != nil {
		t.Fatal(err)
	}
	mt, reply, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.BinaryMessage || string(reply) != string(audio) {
		t.Fatalf("reply type=%d body=%x", mt, reply)
	}
	if *gotAuth != "Token sk-upstream" {
		t.Fatalf("upstream auth = %q", *gotAuth)
	}
}

func TestRealtimeSessionProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/realtime/transcription_sessions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"sess_1","client_secret":{"value":"ephemeral"}}`))
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/realtime/transcription_sessions", `{"model":"m","input_audio_format":"pcm16"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), `"sess_1"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
