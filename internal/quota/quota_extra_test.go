package quota

import "testing"

func TestParsePricing(t *testing.T) {
	// 空串 → 空 map
	m, err := ParsePricing("")
	if err != nil || len(m) != 0 {
		t.Fatalf("empty pricing: %v %v", m, err)
	}
	// 合法 JSON
	p, err := ParsePricing(`{"deepseek-chat":{"input":0.14,"output":0.28},"gpt-5":{"input":1,"output":2}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p["deepseek-chat"].Input != 0.14 || p["gpt-5"].Output != 2 {
		t.Fatalf("parsed wrong: %+v", p)
	}
	// 非法 JSON → 报错
	if _, err := ParsePricing(`{not json`); err == nil {
		t.Fatal("invalid json should error")
	}
}

func TestEstimateCost(t *testing.T) {
	pricing := map[string]ModelPrice{"a": {Input: 0.1, Output: 0.2}}
	if got := EstimateCost(pricing, "a", 1_000_000, 500_000); got != 0.2 {
		t.Fatalf("calc: got %v want 0.2", got)
	}
	// 模型不在定价表 → 0（未配置模型计费通常是零成本信任放行）
	if got := EstimateCost(pricing, "missing", 1000, 1000); got != 0 {
		t.Fatalf("missing model should cost 0, got %v", got)
	}
}
