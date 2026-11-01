package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	_ "llmgate/internal/adapter/deepgram" // 注册 deepgram adapter（预录制 POST /v1/listen 需要）
)

func TestJoinTranscript(t *testing.T) {
	cases := []struct{ base, seg, want string }{
		{"", "hello", "hello"},
		{"hello", "", "hello"},
		{"hello world", "world how are you", "hello world how are you"},
		{"hello world", "Hello World", "hello world"},
		{"a b c", "c d", "a b c d"},
		{"a b", "x y", "a b x y"},
	}
	for _, c := range cases {
		if got := joinTranscript(c.base, c.seg); got != c.want {
			t.Errorf("joinTranscript(%q,%q) = %q, want %q", c.base, c.seg, got, c.want)
		}
	}
}

func TestDeepgramAggregateWS(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		msgs := []string{
			`{"is_final":false,"results":{"channels":[{"alternatives":[{"transcript":"hello wo"}]}]}}`,
			`{"is_final":true,"results":{"channels":[{"alternatives":[{"transcript":"hello world"}]}]}}`,
			`{"is_final":false,"results":{"channels":[{"alternatives":[{"transcript":"how are you"}]}]}}`,
		}
		for _, m := range msgs {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(m)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	if _, err := e.db.Exec(`UPDATE channels SET adapter='deepgram'`); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(e.uh)
	defer gw.Close()

	wsURL := "ws" + gw.URL[len("http"):] + "/v1/listen?model=m&aggregate=1&api_key=leak"
	h := http.Header{"Authorization": {"Token " + testToken}}
	client, _, err := websocket.DefaultDialer.Dial(wsURL, h)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()
	if err := client.WriteMessage(websocket.BinaryMessage, []byte{0x01}); err != nil {
		t.Fatal(err)
	}

	var last map[string]any
	for i := 0; i < 3; i++ {
		_, msg, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if err := json.Unmarshal(msg, &last); err != nil {
			t.Fatalf("unmarshal %d: %v", i, err)
		}
	}
	if got, _ := last["gateway_transcript"].(string); got != "hello world how are you" {
		t.Fatalf("gateway_transcript = %q", got)
	}
	if final, _ := last["gateway_final"].(bool); final {
		t.Fatalf("gateway_final should be false for partial segment")
	}
	// 网关专用参数不得透传上游。
	if strings.Contains(gotQuery, "aggregate") || strings.Contains(gotQuery, "api_key") {
		t.Fatalf("upstream query leaked gateway params: %q", gotQuery)
	}
}

func TestEphemeralTokenRejectedOnHTTP(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/realtime/transcription_sessions", `{"model":"m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d", rec.Code)
	}
	var sess struct {
		ClientSecret struct {
			Value string `json:"value"`
		} `json:"client_secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+sess.ClientSecret.Value)
	rec2 := httptest.NewRecorder()
	e.uh.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("ek_ on HTTP status = %d, body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestDeepgramPrerecorded(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotCT = r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"duration":2.5},"results":{"channels":[]}}`))
	}))
	defer srv.Close()

	e := newTestEnv(t, srv.URL)
	if _, err := e.db.Exec(`UPDATE channels SET adapter='deepgram'`); err != nil {
		t.Fatal(err)
	}
	audio := []byte("fake-wav-bytes")
	rec := e.doWithHeaders(http.MethodPost, "/v1/listen?model=m&language=en", string(audio), map[string]string{
		"Content-Type": "audio/wav",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/listen?language=en&model=m" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotAuth != "Token sk-upstream" {
		t.Fatalf("upstream auth = %q", gotAuth)
	}
	if gotCT != "audio/wav" {
		t.Fatalf("upstream content-type = %q", gotCT)
	}
	if string(gotBody) != string(audio) {
		t.Fatalf("upstream body = %q", gotBody)
	}
	// 2.5s * 1000 tokens/s，计入消费者 in_spent。
	var spent int64
	if err := e.db.QueryRow(`SELECT in_spent FROM users WHERE id=?`, e.userID).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if spent != 2500 {
		t.Fatalf("in_spent = %d, want 2500", spent)
	}
}

func TestDeepgramPrerecordedNoDeepgramChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doWithHeaders(http.MethodPost, "/v1/listen?model=m", "x", map[string]string{
		"Content-Type": "audio/wav",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no deepgram channel") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
