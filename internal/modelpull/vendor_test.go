package modelpull

import "testing"

func TestInferVendor(t *testing.T) {
	cases := map[string]string{
		"deepseek-v4-flash":   "deepseek",
		"deepseek-chat":       "deepseek",
		"gpt-5.6-luna":        "openai",
		"o4-mini":             "openai",
		"claude-sonnet-4.6":   "anthropic",
		"gemini-2.5-flash":    "google",
		"kimi-k2.7-code":      "moonshotai",
		"glm-5.3-flash":       "z-ai",
		"qwen3.8-flash":       "qwen",
		"minimax-m3":          "minimax",
		"grok-4.6":            "x-ai",
		"llama-4-scout-17b":   "meta",
		"doubao-seed-2.0-pro": "bytedance",
		"hy3":                 "tencent",
		"ling-3.0-flash":      "inclusionai",
		"gpt":                 "openai",
		"opencode/zen-go":     "", // 已带前缀不推断
		"space-bunny-free":    "", // 未知厂商
	}
	for model, want := range cases {
		if got := InferVendor(model); got != want {
			t.Fatalf("InferVendor(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestNamespacedName(t *testing.T) {
	if got := NamespacedName("deepseek-v4-flash"); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("NamespacedName = %q", got)
	}
	if got := NamespacedName("openai/gpt-5"); got != "openai/gpt-5" {
		t.Fatalf("NamespacedName(already prefixed) = %q", got)
	}
	if got := NamespacedName("space-bunny-free"); got != "space-bunny-free" {
		t.Fatalf("NamespacedName(unknown) = %q", got)
	}
}
