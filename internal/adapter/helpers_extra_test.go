package adapter

import (
	"context"
	"testing"

	"llmgate/internal/models"
)

func TestStatusError(t *testing.T) {
	e := &StatusError{StatusCode: 502, Body: []byte(`{"error":"up"}`)}
	if got := e.Error(); got != "upstream status 502" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestTestRequest(t *testing.T) {
	r := TestRequest()
	if r.Method != "GET" || r.Path != "/v1/models" {
		t.Fatalf("test req: %s %s", r.Method, r.Path)
	}
	if _, ok := r.Headers["X-Test"]; ok {
		t.Fatal("headers should be empty map")
	}
}

func TestChannelFrom(t *testing.T) {
	ch := &models.Channel{BaseURL: "http://x", TimeoutMS: 5000}
	bc := ChannelFrom(ch)
	if bc.BaseURL != "http://x" || bc.TimeoutMS != 5000 {
		t.Fatalf("ChannelFrom = %+v", bc)
	}
}

func TestParseModelsErrors(t *testing.T) {
	// 非法 JSON
	if _, err := ParseModels([]byte(`{bad`)); err == nil {
		t.Fatal("invalid json should error")
	}
	// 空 data
	out, err := ParseModels([]byte(`{"data":[]}`))
	if err != nil || len(out) != 0 {
		t.Fatalf("empty data: %v %v", out, err)
	}
	// 空 id 被过滤
	out, _ = ParseModels([]byte(`{"data":[{"id":""},{"id":"m1"}]}`))
	if len(out) != 1 || out[0] != "m1" {
		t.Fatalf("blank ids not filtered: %v", out)
	}
}

func TestEscapeJSON(t *testing.T) {
	if got := escapeJSON(`a"b\c`); got != `a\"b\\c` {
		t.Fatalf("escapeJSON = %q", got)
	}
}

// 仅保证 fakeAdapter 接口契约（供其它测试复用对 Registry 的理解）。
func TestRegistryContract(t *testing.T) {
	if Get("__missing__") != nil {
		t.Fatal("missing adapter should be nil")
	}
	_ = context.Background()
}
