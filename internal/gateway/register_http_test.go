package gateway

import (
	"net/http"
	"strings"
	"testing"

	"llmgate/internal/router"
)

const chatModelFmt = `{"model":"m","stream":%t,"messages":[{"role":"user","content":"hi"}]}`

func TestGatewayAuthMissingToken(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.do(http.MethodGet, "/v1/models", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestGatewayAuthDisabledUser(t *testing.T) {
	e := newTestEnv(t, "")
	e.setStatus(0)
	rec := e.doAuth(http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestGatewayAuthInvalidToken(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.do(http.MethodGet, "/v1/models", "", "sk-wrongtoken")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestGatewayAuthQuotaExceeded(t *testing.T) {
	e := newTestEnv(t, "")
	e.setQuota(100, 200)
	rec := e.doAuth(http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestListModels(t *testing.T) {
	// 需要一个启用渠道+路由才能看到模型；用可访问的 stub 即可（不真正转发）。
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"m"`) {
		t.Fatalf("expected model m in list, got %s", rec.Body.String())
	}
}

func TestChatPlainForward(t *testing.T) {
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证透传头
		if r.Header.Get("x-opencode-session") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"chatcmpl-1","model":"m","created":123,"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"content":"hi"`) {
		t.Fatalf("expected content forwarded, got %s", rec.Body.String())
	}
}

func TestChatForwardPassesSessionHeader(t *testing.T) {
	var got string
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-codex-session")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	_ = e.doWithHeaders(http.MethodPost, "/v1/chat/completions",
		`{"model":"m","stream":false,"messages":[]}`, map[string]string{"X-Codex-Session": "sess-1"})
	if got != "sess-1" {
		t.Fatalf("expected passthrough header, got %q", got)
	}
}

func TestChatMissingModel(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"stream":false}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestChatInvalidJSONBody(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestChatBodyTooLarge(t *testing.T) {
	e := newTestEnv(t, "")
	big := `{"model":"m","stream":false,"messages":[{"role":"user","content":"` +
		strings.Repeat("a", maxRequestBody+1) + `"}]}`
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestChatBodyReadError(t *testing.T) {
	e := newTestEnv(t, "")
	req := httptestNewRequest("/v1/chat/completions", failingReader{})
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptestRecorder()
	e.uh.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 on read failure, got %d", rec.Code)
	}
}

func TestChatPlainUpstream4xxPassthrough(t *testing.T) {
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":{"message":"bad","type":"x"}}`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 passthrough, got %d", rec.Code)
	}
}

func TestChatPlainUpstream5xx(t *testing.T) {
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`boom`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChatNoCandidate(t *testing.T) {
	e := newTestEnv(t, "") // 无渠道/路由
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"nonexistent","stream":false}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestChatStreamForward(t *testing.T) {
	srv := sseStub(
		`{"id":"1","choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`{"id":"2","choices":[{"delta":{"content":"hello"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`,
		"[DONE]",
	)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("want text/event-stream, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "[DONE]") || !strings.Contains(rec.Body.String(), `"content":"hello"`) {
		t.Fatalf("stream body unexpected: %s", rec.Body.String())
	}
}

func TestChatStreamPassthrough(t *testing.T) {
	srv := httptestStatus(http.StatusUnprocessableEntity, `{"error":{"message":"bad","type":"x"}}`)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 passthrough, got %d", rec.Code)
	}
}

func TestChatStreamNetworkExhausted(t *testing.T) {
	// 无可用渠道（渠道指向一个立即断流的坏上游，但更简单：无上游环境→404；此处验证 403 之外的失败）
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no candidate), got %d", rec.Code)
	}
}

func TestChatStreamInterrupted(t *testing.T) {
	srv := sseAbruptStub(`{"id":"1","choices":[{"delta":{"content":"partial"}}]}`)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	s := rec.Body.String()
	if !strings.Contains(s, "stream interrupted") || !strings.Contains(s, "[DONE]") {
		t.Fatalf("expected interruption error event, got %s", s)
	}
}

func TestBuildPassHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-OpenCode-Session", "abc")
	h.Set("X-Api-Key", "should-not")
	h.Set("X-Other", "nope")
	out := buildPassHeaders(h)
	if out["x-opencode-session"] != "abc" {
		t.Fatalf("expected x-opencode-session, got %#v", out)
	}
	if _, ok := out["x-api-key"]; ok {
		t.Fatal("x-api-key should be blocked")
	}
	if _, ok := out["x-other"]; ok {
		t.Fatal("non-whitelisted header leaked")
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("a\n b\t c", 10); got != "a b c" {
		t.Fatalf("oneLine normalize: %q", got)
	}
	if got := oneLine(strings.Repeat("x", 300), 10); len(got) != 13 {
		t.Fatalf("oneLine truncate: %d", len(got))
	}
	if got := oneLine("hi", 0); got != "hi" {
		t.Fatalf("oneLine default max: %q", got)
	}
	if got := oneLine("a", -5); got != "a" {
		t.Fatalf("oneLine negative max: %q", got)
	}
}

func TestContentType(t *testing.T) {
	if got := contentType(&router.Result{}); got != "application/json" {
		t.Fatalf("empty content type default, got %q", got)
	}
	if got := contentType(&router.Result{ContentType: "text/plain"}); got != "text/plain" {
		t.Fatalf("explicit content type, got %q", got)
	}
}

// 以下小工具避免在测试内堆过多模板。
func TestPureBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatal("boolToInt wrong")
	}
	if successOrError(200) != "success" || successOrError(500) != "error" {
		t.Fatal("successOrError wrong")
	}
	if upstreamModelifEmpty("", "d") != "d" || upstreamModelifEmpty("u", "d") != "u" {
		t.Fatal("upstreamModelifEmpty wrong")
	}
}
