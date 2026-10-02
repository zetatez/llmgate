package main

import "testing"

func TestShutdownGraceSec(t *testing.T) {
	// 未设置 → 默认 280
	t.Setenv("LGM_SHUTDOWN_GRACE", "")
	if got := shutdownGraceSec(); got != 280 {
		t.Fatalf("default = %d want 280", got)
	}
	// 显式设置
	t.Setenv("LGM_SHUTDOWN_GRACE", "5")
	if got := shutdownGraceSec(); got != 5 {
		t.Fatalf("explicit = %d want 5", got)
	}
	// 非法值 → 回落默认
	t.Setenv("LGM_SHUTDOWN_GRACE", "abc")
	if got := shutdownGraceSec(); got != 280 {
		t.Fatalf("invalid = %d want 280", got)
	}
	// 非正数 → 回落默认
	t.Setenv("LGM_SHUTDOWN_GRACE", "0")
	if got := shutdownGraceSec(); got != 280 {
		t.Fatalf("zero = %d want 280", got)
	}
}
