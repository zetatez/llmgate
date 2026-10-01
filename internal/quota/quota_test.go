package quota

import "testing"

func TestEstimateCost(t *testing.T) {
	pricing := map[string]ModelPrice{
		"deepseek-chat": {Input: 0.14, Output: 0.28},
	}
	// 12 输入 × 0.14/M + 7 输出 × 0.28/M
	got := EstimateCost(pricing, "deepseek-chat", 12, 7)
	want := 12.0/1e6*0.14 + 7.0/1e6*0.28
	if diff := got - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("got %v, want %v", got, want)
	}
	// 未配置的模型 -> 0
	if v := EstimateCost(pricing, "unknown-model", 100, 200); v != 0 {
		t.Fatalf("expected 0 for unpriced model, got %v", v)
	}
	// 空定价表 -> 0
	if v := EstimateCost(nil, "deepseek-chat", 1, 1); v != 0 {
		t.Fatalf("expected 0 for nil pricing, got %v", v)
	}
}

func TestParsePricing(t *testing.T) {
	m, err := ParsePricing(`{"a": {"input":1,"output":2}}`)
	if err != nil || m["a"].Input != 1 || m["a"].Output != 2 {
		t.Fatalf("ParsePricing failed: %v %v", err, m)
	}
	if _, err := ParsePricing("not json"); err == nil {
		t.Fatal("expected error for invalid json")
	}
	if m, _ := ParsePricing(""); len(m) != 0 {
		t.Fatal("expected empty map for empty string")
	}
}
