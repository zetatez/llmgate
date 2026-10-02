package modelpull

import (
	"bytes"
	"context"
	"io"
	"testing"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/config"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

type fakeAdapter struct {
	name string
	mk   func() *adapter.Response // 每次 Do 返回全新响应，避免 reader 被读尽后二次拉取得到空 body
	err  error
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Do(ctx context.Context, ch adapter.BaseChannel, key string, req *adapter.Request) (*adapter.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.mk(), nil
}
func (f *fakeAdapter) DoStream(ctx context.Context, ch adapter.BaseChannel, key string, req *adapter.Request) (io.ReadCloser, error) {
	return nil, nil
}

func newApp(t *testing.T) *app.App {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := app.New(&config.Config{}, db)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return a
}

// newFake 用唯一名无条件注册 adapter，避免全局 Registry 被首个同名注册抢先、测试间串状态。
func newFake(name string, mk func() *adapter.Response, err error) {
	adapter.Register(&fakeAdapter{name: name, mk: mk, err: err})
}

func modelsResp(models ...string) *adapter.Response {
	body := `{"data":[`
	for i, m := range models {
		if i > 0 {
			body += `,`
		}
		body += `{"id":"` + m + `"}`
	}
	body += `]}`
	return &adapter.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(body))), Headers: map[string]string{}}
}

func statusResp(code int, body, ct string) *adapter.Response {
	if ct == "" {
		ct = "application/json"
	}
	return &adapter.Response{StatusCode: code, Body: io.NopCloser(bytes.NewReader([]byte(body))), Headers: map[string]string{"Content-Type": ct}}
}

// addChannel 创建一个带启用 Key 的渠道（adapter 名由调用方给定并与注册的 fake 一致）。
func addChannel(t *testing.T, a *app.App, adapterName string) (*models.Channel, int64) {
	t.Helper()
	chID, err := store.CreateChannel(a.DB, &models.Channel{Name: "c", BaseURL: "http://x", Adapter: adapterName, Weight: 1, TimeoutMS: 1000, Enabled: 1})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	enc, _ := a.Secret.Encrypt("key")
	if err := store.AddChannelKeys(a.DB, chID, []store.KeyInput{{Name: "k", Encrypted: enc}}); err != nil {
		t.Fatalf("add key: %v", err)
	}
	ch, err := store.GetChannel(a.DB, chID)
	if err != nil {
		t.Fatal(err)
	}
	return ch, chID
}

func TestPullChannelCreatesNamespaced(t *testing.T) {
	a := newApp(t)
	ch, chID := addChannel(t, a, "fa1")
	newFake("fa1", func() *adapter.Response { return modelsResp("deepseek-v4-flash", "gpt-5.6-luna", "space-bunny-free") }, nil)

	res, err := PullChannel(context.Background(), a, ch, Options{Priority: 1, Weight: 2})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if res.Total != 3 || res.Created != 3 {
		t.Fatalf("expected 3 created, got %+v", res)
	}
	if ok, _ := store.RouteExists(a.DB, chID, "deepseek/deepseek-v4-flash", "deepseek-v4-flash"); !ok {
		t.Fatal("namespaced route not created")
	}
	if ok, _ := store.RouteExists(a.DB, chID, "space-bunny-free", "space-bunny-free"); !ok {
		t.Fatal("bare route not created")
	}
}

func TestPullChannelSkipsExisting(t *testing.T) {
	a := newApp(t)
	ch, chID := addChannel(t, a, "fa2")
	newFake("fa2", func() *adapter.Response { return modelsResp("deepseek-v4-flash") }, nil)
	if _, err := PullChannel(context.Background(), a, ch, Options{}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.RouteExists(a.DB, chID, "deepseek/deepseek-v4-flash", "deepseek-v4-flash"); !ok {
		t.Fatal("route should exist after first pull")
	}
	res, err := PullChannel(context.Background(), a, ch, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Created != 0 {
		t.Fatalf("expected skipped=1 created=0, got %+v", res)
	}
}

func TestPullChannelRemovesStaleAndBare(t *testing.T) {
	a := newApp(t)
	ch, chID := addChannel(t, a, "fa3")
	store.CreateModelRoute(a.DB, &models.ModelRoute{DisplayName: "deepseek/deepseek-old", ChannelID: chID, UpstreamModel: "deepseek-old", Priority: 0, Weight: 1, Enabled: 1})
	store.CreateModelRoute(a.DB, &models.ModelRoute{DisplayName: "deepseek-v4-flash", ChannelID: chID, UpstreamModel: "deepseek-v4-flash", Priority: 0, Weight: 1, Enabled: 1})

	newFake("fa3", func() *adapter.Response { return modelsResp("deepseek-v4-flash") }, nil)
	res, err := PullChannel(context.Background(), a, ch, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 { // deepseek-old 陈旧被清
		t.Fatalf("stale removal = %d want 1", res.Removed)
	}
	if ok, _ := store.RouteExists(a.DB, chID, "deepseek-v4-flash", "deepseek-v4-flash"); ok {
		t.Fatal("bare route should have been deleted (namespaced naming)")
	}
	if ok, _ := store.RouteExists(a.DB, chID, "deepseek/deepseek-v4-flash", "deepseek-v4-flash"); !ok {
		t.Fatal("namespaced route should exist")
	}
}

func TestPullChannelUnknownAdapter(t *testing.T) {
	a := newApp(t)
	ch, _ := addChannel(t, a, "nope")
	if _, err := PullChannel(context.Background(), a, ch, Options{}); err == nil {
		t.Fatal("unknown adapter should error")
	}
}

func TestPullChannel4xxJSON(t *testing.T) {
	a := newApp(t)
	ch, _ := addChannel(t, a, "fa4")
	newFake("fa4", func() *adapter.Response { return statusResp(400, "bad request", "application/json") }, nil)
	if _, err := PullChannel(context.Background(), a, ch, Options{}); err == nil {
		t.Fatal("4xx should error")
	}
}

func TestPullChannelHTML5xx(t *testing.T) {
	a := newApp(t)
	ch, _ := addChannel(t, a, "fa5")
	newFake("fa5", func() *adapter.Response { return statusResp(500, "<html>nginx</html>", "text/html") }, nil)
	_, err := PullChannel(context.Background(), a, ch, Options{})
	if err == nil || err.Error() == "" {
		t.Fatalf("html error expected, got %v", err)
	}
}

func TestPullChannelEmptyModels(t *testing.T) {
	a := newApp(t)
	ch, _ := addChannel(t, a, "fa6")
	newFake("fa6", func() *adapter.Response { return statusResp(200, `{"data":[]}`, "application/json") }, nil)
	if _, err := PullChannel(context.Background(), a, ch, Options{}); err == nil {
		t.Fatal("empty models should error")
	}
}

func TestPullChannelUpstreamIOError(t *testing.T) {
	a := newApp(t)
	ch, _ := addChannel(t, a, "fa7")
	newFake("fa7", nil, context.DeadlineExceeded)
	if _, err := PullChannel(context.Background(), a, ch, Options{}); err == nil {
		t.Fatal("do error should propagate")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("short: %q", got)
	}
	if got := truncate("1234567890ABCDEF", 10); got != "1234567890..." {
		t.Fatalf("truncate: %q", got)
	}
}
