package gateway

import (
	"net/http"
	"strings"
	"testing"
)

func TestResponsesPlainCompleted(t *testing.T) {
	srv := chatStub(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"chatcmpl-1","model":"m","created":123,"choices":[{"index":0,"message":{"role":"assistant","content":"hi","reasoning_content":"think"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	body := `{"model":"m","instructions":"be concise","input":"hello","max_output_tokens":64}`
	rec := e.doAuth(http.MethodPost, "/v1/responses", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	s := rec.Body.String()
	for _, want := range []string{`"object":"response"`, `"status":"completed"`, `"output_text"`, `"summary_text"`, `"input_tokens":5`, `"output_tokens":3`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestResponsesPlainUpstreamNon2xx(t *testing.T) {
	srv := httptestStatus(http.StatusUnprocessableEntity, `{"error":{"type":"bad","message":"nope"}}`)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"m","input":"hi"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 passthrough, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "nope") {
		t.Fatalf("expected upstream error body, got %s", rec.Body.String())
	}
}

func TestResponsesPlainNoCandidate(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"missing","input":"hi"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestResponsesInvalidJSON(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/responses", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestResponsesMissingModel(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"input":"hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestResponsesBodyTooLarge(t *testing.T) {
	e := newTestEnv(t, "")
	big := `{"model":"m","input":"` + strings.Repeat("a", maxRequestBody+1) + `"}`
	rec := e.doAuth(http.MethodPost, "/v1/responses", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestResponsesStreaming(t *testing.T) {
	srv := sseStub(
		`{"id":"1","choices":[{"delta":{"reasoning_content":"think step"}}]}`,
		`{"id":"2","choices":[{"delta":{"content":"hello resp"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`,
		"[DONE]",
	)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"m","input":"hi","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	s := rec.Body.String()
	for _, want := range []string{
		"response.created", "response.in_progress",
		"reasoning_summary_part.added", "reasoning_summary_text.delta",
		"content_part.added", "output_text.delta",
		"output_item.done", "response.completed",
		`"status":"completed"`, "[DONE]",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in stream:\n%s", want, s)
		}
	}
}

func TestResponsesStreamingPassthrough(t *testing.T) {
	srv := httptestStatus(http.StatusUnprocessableEntity, `{"error":{"type":"bad","message":"nope"}}`)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"m","input":"hi","stream":true}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 passthrough, got %d", rec.Code)
	}
}

func TestResponsesStreamingNoCandidate(t *testing.T) {
	e := newTestEnv(t, "")
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"missing","input":"hi","stream":true}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestResponsesStreamingEmptyOutputFallback(t *testing.T) {
	// 上游流只在 Buffered 阶段给出无 choices 的事件，然后立即 [DONE]：buildOutput 应兜底空消息。
	srv := sseStub(
		`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		"[DONE]",
	)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"m","input":"hi","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("expected completed event, got %s", rec.Body.String())
	}
}

func TestResponsesStreamingInterrupted(t *testing.T) {
	// 上游流中断：应发 response.failed 错误终态，避免 SDK 挂起。
	srv := sseAbruptStub(`{"id":"1","choices":[{"delta":{"content":"partial"}}]}`)
	defer srv.Close()
	e := newTestEnv(t, srv.URL)
	rec := e.doAuth(http.MethodPost, "/v1/responses", `{"model":"m","input":"hi","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	s := rec.Body.String()
	if !strings.Contains(s, "response.failed") || !strings.Contains(s, `"status":"failed"`) {
		t.Fatalf("expected response.failed terminal event, got %s", s)
	}
	if !strings.Contains(s, "[DONE]") {
		t.Fatalf("expected [DONE] sentinel, got %s", s)
	}
}
