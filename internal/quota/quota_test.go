package quota

import "testing"

func fp(v float64) *float64 { return &v }

func TestEstimateCostRoute(t *testing.T) {
	global := map[string]ModelPrice{"deepseek-chat": {Input: 0.14, Output: 0.28}}

	// 路由自定义价优先
	custom := EstimateCostRoute(global, fp(1.0), fp(2.0), "deepseek-chat", 1_000_000, 500_000)
	if custom != 2.0 { // 1M*1 + 0.5M*2
		t.Fatalf("custom price: got %v want 2.0", custom)
	}

	// 路由未设价 → 回退全局
	fallback := EstimateCostRoute(global, nil, nil, "deepseek-chat", 500_000, 250_000)
	if want := 0.5*0.14 + 0.25*0.28; fallback != want {
		t.Fatalf("fallback: got %v want %v", fallback, want)
	}

	// 全部未配置 → 0
	zero := EstimateCostRoute(global, nil, nil, "unknown-model", 1000, 1000)
	if zero != 0 {
		t.Fatalf("no price: got %v want 0", zero)
	}

	// 只设 input：input 用自定义，output 为 0（不部分回退全局，语义明确）
	partialInput := EstimateCostRoute(global, fp(9.0), nil, "deepseek-chat", 1_000_000, 1_000_000)
	if partialInput != 9.0 {
		t.Fatalf("partial input: got %v want 9.0", partialInput)
	}
}
