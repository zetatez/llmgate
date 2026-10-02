package router

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/secret"
	"llmgate/internal/store"
)

// wireAdapter 可编程适配器：按渠道 BaseURL 分发到非流式/流式处理器。
// 每个测试注册一个唯一名字的 wireAdapter，渠道通过 base_url 指定行为。
type wireAdapter struct {
	name   string
	do     func(ch adapter.BaseChannel, key string, req *adapter.Request) (*adapter.Response, error)
	stream func(ch adapter.BaseChannel, key string, req *adapter.Request) (io.ReadCloser, error)
}

func (w *wireAdapter) Name() string { return w.name }

func (w *wireAdapter) Do(_ context.Context, ch adapter.BaseChannel, key string, req *adapter.Request) (*adapter.Response, error) {
	return w.do(ch, key, req)
}

func (w *wireAdapter) DoStream(_ context.Context, ch adapter.BaseChannel, key string, req *adapter.Request) (io.ReadCloser, error) {
	return w.stream(ch, key, req)
}

func resp(code int, body string, ctype string) *adapter.Response {
	if ctype == "" {
		ctype = "application/json"
	}
	if body == "" {
		body = `{"ok":true}`
	}
	return &adapter.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Headers:    map[string]string{"Content-Type": ctype},
	}
}

func statusErrAdapter(name string, code int, body string) *wireAdapter {
	return &wireAdapter{
		name: name,
		do: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (*adapter.Response, error) {
			return resp(code, body, "application/json"), nil
		},
		stream: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
			return nil, &adapter.StatusError{StatusCode: code, Body: []byte(body), HeaderMap: map[string]string{"Content-Type": "application/json"}}
		},
	}
}

// tb 是测试用内存 App 构造助手。
type tb struct {
	t   *testing.T
	db  *sql.DB
	mgr *secret.Manager
}

func newTB(t *testing.T) *tb {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	mgr, _, err := secret.New("aabbccddeeff00112233445566778899")
	if err != nil {
		t.Fatal(err)
	}
	return &tb{t: t, db: db, mgr: mgr}
}

// ch 建渠道，返回渠道 id。
func (b *tb) ch(name, baseURL, adapterName string, prio, weight int) int64 {
	b.t.Helper()
	id, err := store.CreateChannel(b.db, &models.Channel{
		Name: name, BaseURL: baseURL, Adapter: adapterName,
		Priority: prio, Weight: weight, TimeoutMS: 3000, Enabled: 1, HealthState: "healthy",
	})
	if err != nil {
		b.t.Fatal(err)
	}
	return id
}

// route 建渠道下的模型路由。
func (b *tb) route(chID int64, display, upstream string, prio, weight int) {
	b.t.Helper()
	if _, err := store.CreateModelRoute(b.db, &models.ModelRoute{
		DisplayName: display, ChannelID: chID, UpstreamModel: upstream,
		Priority: prio, Weight: weight, Enabled: 1,
	}); err != nil {
		b.t.Fatal(err)
	}
}

// key 加一个启用 Key。
func (b *tb) key(chID int64, plain string) int64 {
	b.t.Helper()
	enc, err := b.mgr.Encrypt(plain)
	if err != nil {
		b.t.Fatal(err)
	}
	if err := store.AddChannelKeys(b.db, chID, []store.KeyInput{{Encrypted: enc}}); err != nil {
		b.t.Fatal(err)
	}
	return 1
}

func (b *tb) router() *Router {
	return &Router{db: b.db, secret: b.mgr, pen: NewPenalizer()}
}

func mustBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runAdapter 注册一个适配器，测试结束自动解注册。
func runAdapter(t *testing.T, a adapter.Adapter) {
	t.Helper()
	adapter.Register(a)
	t.Cleanup(func() { delete(adapter.Registry, a.Name()) })
}

// errReader 在读取时报错，模拟响应体读取中断。
type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }
func (e *errReader) Close() error             { return nil }

// ---------------- 非流式 ForwardChat ----------------

func TestForwardChatSuccess(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{
		name: "fc-ok",
		do: func(_ adapter.BaseChannel, key string, req *adapter.Request) (*adapter.Response, error) {
			if key != "sk-ok" {
				t.Fatalf("expect key sk-ok, got %q", key)
			}
			if req.Path != "/v1/chat/completions" {
				t.Fatalf("path = %q", req.Path)
			}
			if !strings.Contains(mustBody(t, req.Body), `"model":"up-ok"`) {
				t.Fatalf("body should be rewritten to upstream model, got %s", mustBody(t, req.Body))
			}
			if req.Headers["x-up"] != "1" || req.Headers["authorization"] != "client" {
				t.Fatalf("merged headers wrong: %v", req.Headers)
			}
			return resp(200, `{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`, "application/json"), nil
		},
		stream: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
			t.Fatal("stream should not be called")
			return nil, nil
		},
	}
	runAdapter(t, a)
	chID := r.ch("ok", "http://stub-ok", "fc-ok", 0, 1)
	r.route(chID, "m", "up-ok", 0, 1)
	// 渠道带附加头：开放头生效、敏感头被阻断
	if _, err := r.db.Exec(`UPDATE channels SET extra_headers=? WHERE id=?`,
		`{"x-up":"1","authorization":"should-be-blocked","host":"ignore"}`, chID); err != nil {
		t.Fatal(err)
	}
	r.key(chID, "sk-ok")

	rt := r.router()
	res, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`), "m", "/v1/chat/completions", map[string]string{"authorization": "client"})
	if err != nil {
		t.Fatalf("ForwardChat: %v", err)
	}
	if res.StatusCode != 200 || !strings.Contains(string(res.Body), "hi") {
		t.Fatalf("unexpected result %+v", res)
	}
	if rt.pen.ChPenalized(chID) {
		t.Fatal("successful channel should be recovered")
	}
	if rt.pen.ModelDenied(chID, "m") {
		t.Fatal("model should not be denied after success")
	}
}

func TestForwardChatFailover500To200(t *testing.T) {
	r := newTB(t)
	size := 0
	a5 := &wireAdapter{name: "fc-500",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			size++
			return resp(500, "boom", "text/plain"), nil
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, errors.New("n/a")
		},
	}
	a2 := &wireAdapter{name: "fc-200",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return resp(200, `{"ok":1}`, "application/json"), nil
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, errors.New("n/a")
		},
	}
	runAdapter(t, a5)
	runAdapter(t, a2)
	c1 := r.ch("bad", "http://stub-bad", "fc-500", 0, 1)
	c2 := r.ch("good", "http://stub-good", "fc-200", 1, 1)
	r.route(c1, "m", "m", 0, 1)
	r.route(c2, "m", "m", 1, 1)
	r.key(c1, "k1")
	r.key(c2, "k2")

	rt := r.router()
	res, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Channel.ID != c2 {
		t.Fatalf("expected failover to channel %d, got %d", c2, res.Channel.ID)
	}
	if res.Retries != 1 {
		t.Fatalf("expect 1 retry, got %d", res.Retries)
	}
	if size != 1 {
		t.Fatalf("high-priority should only be hit once, got %d", size)
	}
	// 500 触发渠道冷却
	if !rt.pen.ChPenalized(c1) {
		t.Fatal("failed channel should be penalized")
	}
	// 低优先级成功渠道不冷却
	if rt.pen.ChPenalized(c2) {
		t.Fatal("successful channel shouldn't be penalized")
	}
}

func TestForwardChatQuota429(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-quota", 429, "insufficient_quota"))
	chID := r.ch("quota", "http://stub-quota", "fc-quota", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !rt.pen.ChPenalized(chID) {
		t.Fatal("quota-exceeded channel must be penalized")
	}
}

func TestForwardChat431RateLimit(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-429", 429, "rate limit"))
	chID := r.ch("rl", "http://stub-rl", "fc-429", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !rt.pen.ChPenalized(chID) {
		t.Fatal("rate-limited channel must be penalized")
	}
}

func TestForwardChat403AccessDenied(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-403", 403, "access_denied"))
	chID := r.ch("deny", "http://stub-deny", "fc-403", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// 403 非额度 → 模型级拒权熔断（而非渠道冷却）
	if !rt.pen.ModelDenied(chID, "m") {
		t.Fatal("model should be denied on 403 access_denied")
	}
	if rt.pen.ChPenalized(chID) {
		t.Fatal("channel should not be cold down for model-level 403")
	}
}

func TestForwardChat401PenalizeKey(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-401", 401, `{"error":"invalid api key"}`))
	chID := r.ch("401", "http://stub-401", "fc-401", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Key 被冷却并被排除
	keys, _ := store.ListChannelKeys(r.db, chID)
	if !rt.pen.KeyPenalized(keys[0].ID) {
		t.Fatal("key should be penalized on 401")
	}
}

func TestForwardChat400FailoverRecover(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-400", 400, `{"error":"model not found"}`))
	chID := r.ch("400", "http://stub-400", "fc-400", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	rt.pen.PenaliseChan(chID, time.Hour) // 人为预冷却，400 应恢复
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if rt.pen.ChPenalized(chID) {
		t.Fatal("400 (model issue) should recover channel, not cool it")
	}
}

func TestForwardChatOther4xxPassthrough(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-422", 422, `{"error":"unprocessable"}`))
	chID := r.ch("422", "http://stub-422", "fc-422", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	res, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil {
		t.Fatalf("422 should be passthrough, got err %v", err)
	}
	if res.StatusCode != 422 || !strings.Contains(string(res.Body), "unprocessable") {
		t.Fatalf("unexpected 422 result %+v", res)
	}
}

func TestForwardChatNetworkExhausted(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fc-net",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("connection refused")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, errors.New("n/a")
		},
	}
	runAdapter(t, a)
	chID := r.ch("net", "http://stub-net", "fc-net", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrNetworkExhausted) {
		t.Fatalf("expect ErrNetworkExhausted, got %v", err)
	}
}

func TestForwardChatBodyReadFailed(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fc-readfail",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return &adapter.Response{StatusCode: 200, Body: &errReader{err: errors.New("read fail")}, Headers: map[string]string{}}, nil
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, errors.New("n/a")
		},
	}
	runAdapter(t, a)
	chID := r.ch("rf", "http://stub-rf", "fc-readfail", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrNetworkExhausted) {
		t.Fatalf("read failure should be treated as network error, got %v", err)
	}
}

func TestForwardChatNoCandidate(t *testing.T) {
	r := newTB(t)
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"ghost"}`), "ghost", "/p", nil)
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("expect ErrNoCandidate, got %v", err)
	}
}

func TestForwardChatAllModelDeniedBusy(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-busy", 429, "ok"))
	c1 := r.ch("b1", "http://stub-b1", "fc-busy", 0, 1)
	c2 := r.ch("b2", "http://stub-b2", "fc-busy", 1, 1)
	r.route(c1, "m", "m", 0, 1)
	r.route(c2, "m", "m", 1, 1)
	r.key(c1, "k")
	r.key(c2, "k")
	rt := r.router()
	rt.pen.DenyModel(c1, "m", time.Hour)
	rt.pen.DenyModel(c2, "m", time.Hour)
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("expect ErrChannelBusy, got %v", err)
	}
}

func TestForwardChatCancelledContext(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-cancel", 429, "x"))
	chID := r.ch("cx", "http://stub-cx", "fc-cancel", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := r.router()
	_, err := rt.ForwardChat(ctx, []byte(`{"model":"m"}`), "m", "/p", nil)
	if err == nil || !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("expect cancelled error, got %v", err)
	}
}

func TestForwardChatNoKey(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fc-nokey", 429, "x"))
	chID := r.ch("nk", "http://stub-nk", "fc-nokey", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	// 没有 Key
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("no-enabled-key channel is treated as unusable, got %v", err)
	}
}

func TestForwardChatAdapterNotFound(t *testing.T) {
	r := newTB(t)
	chID := r.ch("na", "http://stub-na", "does-not-exist", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChat(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("unknown-adapter channel is treated as unusable, got %v", err)
	}
}

// ---------------- 流式 ForwardChatStream ----------------

func TestForwardChatStreamSuccess(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fs-ok",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			t.Fatal("non-stream not used")
			return nil, nil
		},
		stream: func(_ adapter.BaseChannel, _ string, _ *adapter.Request) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data: {\"c\":\"hi\"}\n\n")), nil
		},
	}
	runAdapter(t, a)
	chID := r.ch("sok", "http://stub-sok", "fs-ok", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	sr, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sr.StatusCode != http.StatusOK || !strings.Contains(string(sr.Buffered), "data:") {
		t.Fatalf("unexpected stream result %+v", sr)
	}
	sr.Stream.Close()
	if rt.pen.ChPenalized(chID) {
		t.Fatal("stream success should recover channel")
	}
}

func TestForwardChatStreamDiedBeforeFirstEvent(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fs-die",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("")), nil // 立即 EOF，无 data
		},
	}
	runAdapter(t, a)
	chID := r.ch("sdie", "http://stub-sdie", "fs-die", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrNetworkExhausted) {
		t.Fatalf("expect ErrNetworkExhausted for dead stream, got %v", err)
	}
	if !rt.pen.ChPenalized(chID) {
		t.Fatal("dead stream channel should be penalized")
	}
}

func TestForwardChatStreamProtoUnsupported(t *testing.T) {
	r := newTB(t)
	protoBody := `{"type":"error","error":{"type":"ModelProtocolUnsupported","message":"nope"}}`
	a := &wireAdapter{name: "fs-proto",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, &adapter.StatusError{StatusCode: 400, Body: []byte(protoBody), HeaderMap: map[string]string{"Content-Type": "text/plain"}}
		},
	}
	runAdapter(t, a)
	chID := r.ch("sproto", "http://stub-sproto", "fs-proto", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	sr, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil {
		t.Fatalf("proto should passthrough, got %v", err)
	}
	if sr.StatusCode != 400 || !strings.Contains(string(sr.Passthrough), "ModelProtocolUnsupported") {
		t.Fatalf("unexpected proto passthrough %+v", sr)
	}
	if !rt.pen.ModelDenied(chID, "m") {
		t.Fatal("model should be denied on proto unsupported")
	}
	// proto 错误应恢复渠道（不冷却整条渠道）
	if rt.pen.ChPenalized(chID) {
		t.Fatal("channel should be recovered on proto error")
	}
}

func TestForwardChatStream500Failover(t *testing.T) {
	r := newTB(t)
	a5 := &wireAdapter{name: "fs-500",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, &adapter.StatusError{StatusCode: 500, Body: []byte("boom"), HeaderMap: map[string]string{}}
		},
	}
	a2 := &wireAdapter{name: "fs-200",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data: hi\n\n")), nil
		},
	}
	runAdapter(t, a5)
	runAdapter(t, a2)
	c1 := r.ch("sbad", "http://stub-sbad", "fs-500", 0, 1)
	c2 := r.ch("sgood", "http://stub-sgood", "fs-200", 1, 1)
	r.route(c1, "m", "m", 0, 1)
	r.route(c2, "m", "m", 1, 1)
	r.key(c1, "k")
	r.key(c2, "k")
	rt := r.router()
	sr, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sr.Channel.ID != c2 || sr.Retries != 1 {
		t.Fatalf("expected failover to %d with retries 1, got %+v", c2, sr)
	}
	sr.Stream.Close()
}

func TestForwardChatStreamOther4xxPassthrough(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fs-422",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, &adapter.StatusError{StatusCode: 422, Body: []byte(`{"error":"x"}`), HeaderMap: map[string]string{"Content-Type": "application/json"}}
		},
	}
	runAdapter(t, a)
	chID := r.ch("s422", "http://stub-s422", "fs-422", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	sr, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if err != nil {
		t.Fatalf("422 should passthrough, got err %v", err)
	}
	if sr.StatusCode != 422 || !strings.Contains(string(sr.Passthrough), "x") {
		t.Fatalf("unexpected 422 stream passthrough %+v", sr)
	}
}

func TestForwardChatStreamNoCandidate(t *testing.T) {
	r := newTB(t)
	rt := r.router()
	_, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"ghost"}`), "ghost", "/p", nil)
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("expect ErrNoCandidate, got %v", err)
	}
}

func TestForwardChatStreamAllModelDeniedBusy(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fs-busy", 429, "x"))
	c1 := r.ch("y1", "http://stub-y1", "fs-busy", 0, 1)
	c2 := r.ch("y2", "http://stub-y2", "fs-busy", 1, 1)
	r.route(c1, "m", "m", 0, 1)
	r.route(c2, "m", "m", 1, 1)
	r.key(c1, "k")
	r.key(c2, "k")
	rt := r.router()
	rt.pen.DenyModel(c1, "m", time.Hour)
	rt.pen.DenyModel(c2, "m", time.Hour)
	_, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("expect ErrChannelBusy, got %v", err)
	}
}

func TestForwardChatStreamNetworkError(t *testing.T) {
	r := newTB(t)
	a := &wireAdapter{name: "fs-net",
		do: func(adapter.BaseChannel, string, *adapter.Request) (*adapter.Response, error) {
			return nil, errors.New("n/a")
		},
		stream: func(adapter.BaseChannel, string, *adapter.Request) (io.ReadCloser, error) {
			return nil, errors.New("conn reset")
		},
	}
	runAdapter(t, a)
	chID := r.ch("sn", "http://stub-sn", "fs-net", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	r.key(chID, "k")
	rt := r.router()
	_, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrNetworkExhausted) {
		t.Fatalf("expect ErrNetworkExhausted, got %v", err)
	}
}

func TestForwardChatStreamNoKey(t *testing.T) {
	r := newTB(t)
	runAdapter(t, statusErrAdapter("fs-nokey", 429, "x"))
	chID := r.ch("snk", "http://stub-snk", "fs-nokey", 0, 1)
	r.route(chID, "m", "m", 0, 1)
	rt := r.router()
	_, err := rt.ForwardChatStream(context.Background(), []byte(`{"model":"m"}`), "m", "/p", nil)
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("no-enabled-key stream channel treated as unusable, got %v", err)
	}
}

// ---------------- 结果/工具函数 ----------------

func TestNew(t *testing.T) {
	r := newTB(t)
	a := &app.App{DB: r.db, Secret: r.mgr}
	// pen == nil → 内部自建
	rt := New(a, nil)
	if rt.pen == nil {
		t.Fatal("expected internal penalizer created")
	}
	if rt.db != r.db || rt.secret != r.mgr {
		t.Fatal("router should hold injected db and secret")
	}
	// 显式注入共享熔断器
	shared := NewPenalizer()
	rt2 := New(a, shared)
	if rt2.pen != shared {
		t.Fatal("expected shared penalizer to be used")
	}
}

func TestRewriteModel(t *testing.T) {
	out := rewriteModel([]byte(`{"model":"orig","x":1}`), "up/stream")
	if !strings.Contains(string(out), `"model":"up/stream"`) || !strings.Contains(string(out), `"x":1`) {
		t.Fatalf("rewriteModel changed structure: %s", out)
	}
	if strings.Contains(string(out), `"orig"`) {
		t.Fatalf("original model should be gone: %s", out)
	}
	// 非法 JSON → 原样返回
	if string(rewriteModel([]byte(`{not-json`), "x")) != `{not-json` {
		t.Fatal("invalid json should pass through unchanged")
	}
}

func TestChannelHeadersFilter(t *testing.T) {
	ch := &models.Channel{ExtraHeaders: `{"x-good":"1","authorization":"block","X-Api-Key":"k","x-empty":"","":"v"}`}
	h := channelHeaders(ch)
	if h["x-good"] != "1" {
		t.Fatalf("x-good should pass, got %v", h)
	}
	for _, k := range []string{"authorization", "x-api-key", "x-empty", ""} {
		if _, ok := h[k]; ok {
			t.Fatalf("header %q should be filtered, got %v", k, h)
		}
	}
	// 非法 JSON / 空 → 空 map
	if len(channelHeaders(&models.Channel{ExtraHeaders: `{nope`})) != 0 {
		t.Fatal("invalid JSON should yield empty headers")
	}
	if len(channelHeaders(nil)) != 0 {
		t.Fatal("nil channel should yield empty headers")
	}
}

func TestMergeHeaders(t *testing.T) {
	out := mergeHeaders(map[string]string{"a": "1", "b": "2"}, map[string]string{"b": "9", "c": "3"})
	if out["a"] != "1" || out["b"] != "9" || out["c"] != "3" {
		t.Fatalf("mergeHeaders mismatch: %v", out)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 20) != "short" {
		t.Fatal("short string should be unchanged")
	}
	if truncate("1234567890", 5) != "12345..." {
		t.Fatal("long string should be truncated with ellipsis")
	}
}

func TestWeightOf(t *testing.T) {
	if weightOf(0) != 1 || weightOf(-5) != 1 || weightOf(7) != 7 {
		t.Fatal("weightOf clamping wrong")
	}
}

func TestPenalizeKeySkipNonPositive(t *testing.T) {
	p := NewPenalizer()
	p.PenalizeKey(-1, time.Minute) // 应被忽略，不 panic
	if p.KeyPenalized(0) {
		t.Fatal("no key should be penalized")
	}
}

func TestPenalizeKeyDefaultDuration(t *testing.T) {
	p := NewPenalizer()
	p.PenalizeKey(5, 0) // d<=0 → 默认 30s
	if !p.KeyPenalized(5) {
		t.Fatal("key 5 should be penalized")
	}
}

func TestPenaliperConcurrency(t *testing.T) {
	p := NewPenalizer()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(n int) {
			p.PenaliseChan(int64(n%5), 0)
			p.ChPenalized(int64(n % 5))
			p.DenyModel(int64(n%5), "m", time.Minute)
			p.ModelDenied(int64(n%5), "m")
			p.PenalizeKey(int64(n%5), time.Minute)
			p.KeyPenalized(int64(n % 5))
			p.ClearChan(int64(n % 5))
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
