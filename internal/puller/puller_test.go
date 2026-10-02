package puller

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/config"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

type fakeAdapter struct {
	name string
	mk   func() *adapter.Response // 每次 Do 返回全新响应，避免二次拉取拿到已被读尽的 reader
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

func newPuller(t *testing.T, chs ...func(a *app.App)) (*Puller, *app.App, *sql.DB) {
	t.Helper()
	cfg := &config.Config{}
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := app.New(cfg, db)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	pen := router.NewPenalizer()
	// 注册 fake adapter
	if adapter.Get("testpull") == nil {
		adapter.Register(&fakeAdapter{name: "testpull", mk: func() *adapter.Response { return modelsResp("deepseek-v4-flash") }})
	}
	for _, setup := range chs {
		setup(a)
	}
	return New(a, pen), a, db
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
	return &adapter.Response{StatusCode: 200, Body: io.NopCloser(reader(body)), Headers: map[string]string{}}
}

func reader(s string) *strReader { return &strReader{s: s, r: 0} }

type strReader struct {
	s string
	r int
}

func (r *strReader) Read(p []byte) (int, error) {
	if r.r >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.r:])
	r.r += n
	return n, nil
}
func (r *strReader) Close() error { return nil }

func addEnabledChannel(a *app.App, name string) int64 {
	chID, _ := store.CreateChannel(a.DB, &models.Channel{Name: name, BaseURL: "http://x", Adapter: "testpull", Weight: 1, TimeoutMS: 1000, Enabled: 1, HealthState: "healthy"})
	enc, _ := a.Secret.Encrypt("key")
	_ = store.AddChannelKeys(a.DB, chID, []store.KeyInput{{Name: "k", Encrypted: enc}})
	return chID
}

func TestTickCreatesRoutes(t *testing.T) {
	p, a, _ := newPuller(t, func(a *app.App) { addEnabledChannel(a, "c1") })
	res := p.SyncNow() // 直接调用 tick（New 未启动协程）
	if res["created"] != 1 {
		t.Fatalf("created = %v want 1", res["created"])
	}
	// 再次同步 → skipped
	res = p.SyncNow()
	if res["skipped"] != 1 {
		t.Fatalf("skipped = %v want 1", res["skipped"])
	}
	// 记录 model_pull_last
	if v, _ := a.GetSetting("model_pull_last"); v == "" {
		t.Fatal("model_pull_last not recorded")
	}
}

func TestTickSkipsDisabledAndCooldown(t *testing.T) {
	p, _, _ := newPuller(t, func(a *app.App) {
		store.CreateChannel(a.DB, &models.Channel{Name: "disabled", BaseURL: "http://x", Adapter: "testpull", Weight: 1, TimeoutMS: 1000, Enabled: 0})
		id := addEnabledChannel(a, "cd")
		_ = store.SetChannelCooldown(a.DB, id, models.Now()+999999)
	})
	res := p.SyncNow()
	if res["created"] != 0 {
		t.Fatalf("disabled/cooldown channels should be skipped, got %+v", res)
	}
}

func TestTickPropagatesPullError(t *testing.T) {
	// 覆盖 adapter 报错通道：注册一个返回错误的 adapter
	adapter.Register(&fakeAdapter{name: "testpull-err", mk: nil, err: context.DeadlineExceeded})
	p, _, _ := newPuller(t, func(a *app.App) {
		store.CreateChannel(a.DB, &models.Channel{Name: "errc", BaseURL: "http://x", Adapter: "testpull-err", Weight: 1, TimeoutMS: 1000, Enabled: 1, HealthState: "healthy"})
	})
	res := p.SyncNow()
	if res["errored"] != 1 {
		t.Fatalf("errored = %v want 1", res["errored"])
	}
}

func TestSafeRunRecoversPanic(t *testing.T) {
	p := &Puller{}
	ran := false
	p.safeRun("boom", func() {
		ran = true
		panic("oops")
	})
	// safeRun 捕获 panic，不崩溃，但 fn 已执行（panic 在 fn 内）
	if !ran {
		t.Fatal("fn should have run")
	}
	p.safeRun("ok", func() {})
}

func TestIntSettingAndIntervalMin(t *testing.T) {
	p, a, _ := newPuller(t)
	if p.intervalMin() != defaultIntervalMin {
		t.Fatalf("default interval = %v", p.intervalMin())
	}
	_ = a.SetSetting("pull_interval_min", "15")
	if p.intervalMin() != 15 {
		t.Fatalf("interval from setting = %v", p.intervalMin())
	}
	_ = a.SetSetting("pull_interval_min", "abc")
	if p.intervalMin() != defaultIntervalMin {
		t.Fatalf("bad int should fallback to default")
	}
	if got := intSetting(a, "missing", 7); got != 7 {
		t.Fatalf("missing setting default = %v", got)
	}
}

func TestProbeOnceRestoresHealthy(t *testing.T) {
	p, a, _ := newPuller(t, func(a *app.App) {
		id := addEnabledChannel(a, "cd")
		_ = store.SetChannelCooldown(a.DB, id, time.Now().Add(-time.Minute).Unix())
	})
	p.probeOnce()
	chs, _ := store.ListChannels(a.DB)
	for _, c := range chs {
		if c.HealthState != "healthy" {
			t.Fatalf("cooldown channel should be restored, got %q", c.HealthState)
		}
	}
}

func TestProbeOnceSkipsDisabled(t *testing.T) {
	p, _, _ := newPuller(t, func(a *app.App) {
		id, _ := store.CreateChannel(a.DB, &models.Channel{Name: "d", BaseURL: "http://x", Adapter: "testpull", Weight: 1, TimeoutMS: 1000, Enabled: 0})
		_ = store.SetChannelCooldown(a.DB, id, time.Now().Add(-time.Minute).Unix())
	})
	p.probeOnce() // 不应 panic；disabled 渠道直接恢复 healthy
}

func TestCleanupOnceDeletesOldLogs(t *testing.T) {
	p, a, db := newPuller(t)
	_ = a.SetSetting("log_retain_days", "1")
	// 插入一条很旧的日志
	_ = store.InsertLogs(db, []*models.RequestLog{{UserID: 1, DisplayModel: "m", CreatedAt: models.Now() - 100*86400, Status: "success"}})
	p.cleanupOnce()
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM request_logs`).Scan(&n)
	if n != 0 {
		t.Fatalf("old logs should be deleted, left %d", n)
	}
}

func TestCleanupOnceDisabledWhenDaysZero(t *testing.T) {
	p, a, db := newPuller(t)
	_ = a.SetSetting("log_retain_days", "0")
	_ = store.InsertLogs(db, []*models.RequestLog{{UserID: 1, DisplayModel: "m", CreatedAt: 1, Status: "success"}})
	p.cleanupOnce()
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM request_logs`).Scan(&n)
	if n != 1 {
		t.Fatalf("disabled cleanup should keep logs, left %d", n)
	}
}

func TestRunStop(t *testing.T) {
	p, a, _ := newPuller(t)
	_ = a.SetSetting("pull_interval_min", "60")
	done := make(chan struct{})
	go func() {
		p.Run()
		close(done)
	}()
	// 等 Run 完成一次 sync
	time.Sleep(50 * time.Millisecond)
	p.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run should return after Stop")
	}
}

func TestRunBackgroundFieldsImmediately(t *testing.T) {
	p, _, _ := newPuller(t, func(a *app.App) { addEnabledChannel(a, "bg") })
	// 启动后台，观察第一轮 probe/cleanup 立即执行（不 panic）
	done := make(chan struct{})
	go func() {
		p.RunBackground()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	p.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunBackground should return after Stop")
	}
}
