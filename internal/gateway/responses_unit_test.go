package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"llmgate/internal/models"
)

func TestSSEEmitterEmit(t *testing.T) {
	var buf bytes.Buffer
	em := &sseEmitter{w: &buf}
	em.emit("response.created", map[string]any{"type": "response.created", "x": 1})
	s := buf.String()
	if !strings.HasPrefix(s, "event: response.created\n") || !strings.Contains(s, "data: ") {
		t.Fatalf("bad SSE frame: %q", s)
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Fatalf("frame should end with blank line: %q", s)
	}
}

func TestFlushNil(t *testing.T) {
	flush(nil) // 不应 panic
}

func TestEmitDelTask(t *testing.T) {
	var buf bytes.Buffer
	em := &sseEmitter{w: &buf}
	var acc strings.Builder
	data := []byte("not-data\n" +
		"data: [DONE]\n" +
		"data: {\"bad\n" +
		"data: {\"choices\":[]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\",\"reasoning_content\":\"r\"}}]}\n")
	emitDelTask(em, "item_1", data, &acc)
	s := buf.String()
	if !strings.Contains(s, "output_text.delta") || !strings.Contains(s, "reasoning_summary_text.delta") {
		t.Fatalf("expected both delta events, got %q", s)
	}
	if acc.String() != "hi" {
		t.Fatalf("accumulated content = %q", acc.String())
	}
}

func TestIngestFrameSkipBranches(t *testing.T) {
	var buf bytes.Buffer
	sw := &sseEmitter{w: &buf}
	r := &responsesSSE{sw: sw}
	r.ingestFrame([]byte("event: x\n"))         // 非 data 行
	r.ingestFrame([]byte("data: [DONE]\n\n"))   // 哨兵
	r.ingestFrame([]byte("data: {\"bad\"\n\n")) // 非法 JSON
	r.ingestFrame([]byte("data: {}\n\n"))       // 无 choices
	if buf.Len() != 0 {
		t.Fatalf("expected no events for skip branches, got %q", buf.String())
	}
}

func TestSSEChordWithoutReason(t *testing.T) {
	var buf bytes.Buffer
	sw := &sseEmitter{w: &buf}
	r := &responsesSSE{sw: sw}
	// 只发 content，msgOutputIndex 应回退 0
	r.ingestFrame([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"only\"}}]}\n\n"))
	if r.msgOutputIndex() != 0 {
		t.Fatalf("msg index should be 0 without reason, got %d", r.msgOutputIndex())
	}
	if len(r.buildOutput()) != 1 {
		t.Fatalf("expected single message item, got %d", len(r.buildOutput()))
	}
	r.finalize("m")
	if !bytes.Contains(buf.Bytes(), []byte("output_item.done")) {
		t.Fatalf("missing done event: %q", buf.String())
	}
}

func TestBaseResponse(t *testing.T) {
	br := baseResponse("id1", "disp", "up", statusRunning)
	if br["model"] != "up" || br["status"] != statusRunning {
		t.Fatalf("bad base response: %v", br)
	}
	br2 := baseResponse("id2", "disp", "", statusFailed)
	if br2["model"] != "disp" {
		t.Fatalf("empty upstream should fall back to display model")
	}
}

func TestParseResponsesInputStringAndNull(t *testing.T) {
	if got, err := parseResponsesInput(json.RawMessage(`null`)); err != nil || got != nil {
		t.Fatalf("null should return nil, got %v err=%v", got, err)
	}
	if got, err := parseResponsesInput(nil); err != nil || got != nil {
		t.Fatalf("empty should return nil, got %v err=%v", got, err)
	}
	got, err := parseResponsesInput(json.RawMessage(`"hello"`))
	if err != nil || len(got) != 1 || got[0]["content"] != "hello" {
		t.Fatalf("string input failed: %v %v", got, err)
	}
}

func TestParseResponsesInputItemError(t *testing.T) {
	// 标量（非字符串/数组）→ 报错
	if _, err := parseResponsesInput(json.RawMessage(`123`)); err == nil {
		t.Fatal("expected parse error for scalar item")
	}
}

func TestResponsesItemToChatCases(t *testing.T) {
	// 字符串 content
	m, err := responsesItemToChat(json.RawMessage(`{"role":"user","content":"direct"}`))
	if err != nil || m["content"] != "direct" {
		t.Fatalf("string content failed: %v %v", m, err)
	}
	// 无 role、纯 text
	m2, _ := responsesItemToChat(json.RawMessage(`[{"role":"x"}]`))
	if m2 != nil {
		t.Fatalf("unsupported item should produce nil (via json? actually raw is array) %v", m2)
	}
	// text 字段兜底
	m3, err := responsesItemToChat(json.RawMessage(`{"role":"user","content":{"unexpected":1},"text":"fallback"}`))
	if err != nil || m3["content"] != "fallback" {
		t.Fatalf("text fallback failed: %v %v", m3, err)
	}
	// 空文本
	m4, _ := responsesItemToChat(json.RawMessage(`{"type":"message","role":"assistant","content":[]}`))
	if m4["content"] != "" {
		t.Fatalf("empty content should be empty string, got %v", m4["content"])
	}
}

func TestChatToResponseNoReasoning(t *testing.T) {
	chat := []byte(`{"id":"x","model":"m","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"plain"},"finish_reason":"stop"}]}`)
	l := &models.RequestLog{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	out := chatToResponse(chat, "disp", "up", l)
	s := string(out)
	if strings.Contains(s, "summary_text") {
		t.Fatalf("should not contain reasoning summary, got %s", s)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["model"] != "up" {
		t.Fatalf("model should be upstream, got %v", resp["model"])
	}
}

func TestChatToResponseGeneratesID(t *testing.T) {
	chat := []byte(`{"choices":[{"index":0,"message":{"content":"x"}}]}`)
	out := chatToResponse(chat, "d", "u", &models.RequestLog{})
	var resp map[string]any
	_ = json.Unmarshal(out, &resp)
	if resp["id"] == "" {
		t.Fatal("id should be generated when absent")
	}
}

func TestUsageFromLog(t *testing.T) {
	l := &models.RequestLog{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
	u := usageFromLog(l)
	if u["input_tokens"] != int64(10) || u["output_tokens"] != int64(20) || u["total_tokens"] != int64(30) {
		t.Fatalf("bad usage: %v", u)
	}
}

func TestRandHex(t *testing.T) {
	a, b := randHex(6), randHex(6)
	if a == "" || b == "" || a == b {
		t.Fatalf("randHex should return distinct non-empty: %q %q", a, b)
	}
	if len(randHex(5)) != 2*5 {
		t.Fatalf("expected hex length 10, got %d", len(randHex(5)))
	}
}

func TestResponsesToChatNoMessages(t *testing.T) {
	// 空 input + 空 instructions → 兜底单条 user 空消息
	out, err := responsesToChat(responsesBody{Model: "m", Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	var chat map[string]any
	_ = json.Unmarshal(out, &chat)
	if len(chat["messages"].([]any)) != 1 {
		t.Fatalf("expected 1 fallback message, got %v", chat["messages"])
	}
}
