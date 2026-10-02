package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"llmgate/internal/models"
)

func TestResponsesToChatStringInput(t *testing.T) {
	body := `{"model":"level1","instructions":"be brief","input":"hello","max_output_tokens":64,"stream":true}`
	var rb responsesBody
	if err := json.Unmarshal([]byte(body), &rb); err != nil {
		t.Fatal(err)
	}
	out, err := responsesToChat(rb)
	if err != nil {
		t.Fatal(err)
	}
	var chat map[string]any
	if err := json.Unmarshal(out, &chat); err != nil {
		t.Fatalf("chat body invalid: %v (%s)", err, out)
	}
	messages := chat["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected system+user, got %d", len(messages))
	}
	if messages[0].(map[string]any)["role"] != "system" {
		t.Fatal("system message expected")
	}
	if chat["stream"] != true || chat["max_tokens"] != float64(64) {
		t.Fatalf("stream/max_tokens: %v %v", chat["stream"], chat["max_tokens"])
	}
}

func TestResponsesToChatArrayInput(t *testing.T) {
	body := `{"model":"level1","input":[{"role":"user","content":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]},{"type":"message","role":"user","content":[{"type":"output_text","text":"c"}]}]}`
	var rb responsesBody
	if err := json.Unmarshal([]byte(body), &rb); err != nil {
		t.Fatal(err)
	}
	out, err := responsesToChat(rb)
	if err != nil {
		t.Fatal(err)
	}
	var chat map[string]any
	_ = json.Unmarshal(out, &chat)
	messages := chat["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 user msgs, got %d: %s", len(messages), out)
	}
	first := messages[0].(map[string]any)["content"].(string)
	if first != "ab" {
		t.Fatalf("content parts should concatenate, got %q", first)
	}
}

func TestChatToResponse(t *testing.T) {
	chat := []byte(`{"id":"chatcmpl-x","model":"deepseek-v4-flash","created":1700000000,"choices":[{"index":0,"message":{"role":"assistant","content":"hi","reasoning_content":"think"},"finish_reason":"stop"}]}`)
	l := &models.RequestLog{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	out := chatToResponse(chat, "level1", "deepseek-v4-flash", l)
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["object"] != "response" || resp["status"] != "completed" {
		t.Fatalf("bad response envelope: %s", out)
	}
	output := resp["output"].([]any)[0].(map[string]any)
	if output["type"] != "message" {
		t.Fatalf("output type: %s", out)
	}
	content := output["content"].([]any)
	if !strings.Contains(string(out), `"output_text"`) {
		t.Fatalf("missing output_text: %s", out)
	}
	_ = content
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) {
		t.Fatalf("usage: %v", usage)
	}
}
