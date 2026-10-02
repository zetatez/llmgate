package gateway

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/config"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"

	_ "llmgate/internal/adapter/openai"
	_ "modernc.org/sqlite"
)

const testToken = "sk-test123"

// testEnv 为单个用例组装一份独立环境：内存 SQLite + app + gin 路由（含 openai 上游适配器注册）。
type testEnv struct {
	a      *app.App
	bus    *logbus.Bus
	uh     *gin.Engine
	db     *sql.DB
	userID int64
}

// newTestEnv upstreamURL 非空时同时创建指向该 httptest 的渠道、模型路由与加密 Key。
func newTestEnv(t *testing.T, upstreamURL string) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{EncryptSeed: "test-seed-passphrase", AdminToken: "admin-tok"}
	a, err := app.New(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := store.CreateUser(db, &models.User{
		Name: "tester", TokenHash: app.HashToken(testToken), QuotaLimit: 0, QuotaUsed: 0, Status: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if upstreamURL != "" {
		chID, err := store.CreateChannel(db, &models.Channel{
			Name: "up", BaseURL: upstreamURL, Adapter: "openai", Priority: 0, Weight: 1,
			TimeoutMS: 5000, Enabled: 1, HealthState: "healthy",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateModelRoute(db, &models.ModelRoute{
			DisplayName: "m", ChannelID: chID, UpstreamModel: "m", Priority: 0, Weight: 1, Enabled: 1,
		}); err != nil {
			t.Fatal(err)
		}
		enc, _ := a.Secret.Encrypt("sk-upstream")
		if err := store.AddChannelKeys(db, chID, []store.KeyInput{{Encrypted: enc}}); err != nil {
			t.Fatal(err)
		}
	}
	bus := logbus.New(db, 64)
	t.Cleanup(func() { bus.Close() })
	uh := gin.New()
	Register(uh.Group("/v1"), a, bus, router.NewPenalizer())
	return &testEnv{a: a, bus: bus, uh: uh, db: db, userID: uid}
}

func (e *testEnv) setQuota(limit, used float64) {
	_, _ = e.db.Exec(`UPDATE users SET quota_limit=?, quota_used=? WHERE id=?`, limit, used, e.userID)
}

func (e *testEnv) setStatus(status int) {
	_, _ = e.db.Exec(`UPDATE users SET status=? WHERE id=?`, status, e.userID)
}

func (e *testEnv) do(method, path, body string, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.uh.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) doAuth(method, path, body string) *httptest.ResponseRecorder {
	return e.do(method, path, body, testToken)
}

func (e *testEnv) doWithHeaders(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.uh.ServeHTTP(rec, req)
	return rec
}

// failingReader 让 io.ReadAll 立即返回错误。
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func failingBody() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", failingReader{})
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

func httptestNewRequest(path string, body io.Reader) *http.Request {
	return httptest.NewRequest(http.MethodPost, path, body)
}

func httptestRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// httptestStatus 固定返回指定状态码与响应体。
func httptestStatus(code int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		w.Write([]byte(body))
	}))
}

// chatStub 返回一个标准非流式 OpenAI chat/completions 上游。
func chatStub(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// sseStub 返回一个逐事件刷新的 SSE 上游（data 事件直接给定）。
func sseStub(events ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, e := range events {
			_, _ = w.Write([]byte("data: " + e + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
	}))
}

// sseAbruptStub 输出一个 data 事件后立即掐断连接（无正常 EOF），
// 用于覆盖"上游流中断"分支。
func sseAbruptStub(first string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + first + "\n\n"))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
}
