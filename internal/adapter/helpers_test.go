package adapter

import (
	"encoding/json"
	"io"
	"testing"
)

// HealthRequest 生成的请求体必须是合法 JSON，且模型名中的引号被转义。
func TestHealthRequestBody(t *testing.T) {
	req := HealthRequest("my\"model\\x")
	if req.Method != "POST" || req.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected probe: %s %s", req.Method, req.Path)
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Model    string `json:"model"`
		MaxToken int    `json:"max_tokens"`
		Stream   bool   `json:"stream"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("probe body is not valid JSON: %v (%s)", err, b)
	}
	if parsed.Model != `my"model\x` {
		t.Fatalf("model roundtrip: got %q", parsed.Model)
	}
	if parsed.MaxToken != 1 || parsed.Stream {
		t.Fatalf("probe should be minimal chat: %+v", parsed)
	}
}

// HealthRequest 无模型时回退到 models 列表（历史行为，供未同步渠道探活）。
func TestHealthRequestFallback(t *testing.T) {
	req := HealthRequest("")
	if req.Method != "GET" || req.Path != "/v1/models" {
		t.Fatalf("fallback probe should be GET /v1/models, got %s %s", req.Method, req.Path)
	}
	if req.Body != nil {
		t.Fatal("GET probe should have no body")
	}
}
