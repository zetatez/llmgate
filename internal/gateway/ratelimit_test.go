package gateway

import "testing"

func TestRateLimiterQPS(t *testing.T) {
	rl := NewRateLimiter(2, 0)
	if !rl.Enabled() {
		t.Fatal("qps limit should enable")
	}
	for i := 0; i < 2; i++ {
		if !rl.AllowQPS(7) {
			t.Fatalf("attempt %d should pass", i)
		}
	}
	if rl.AllowQPS(7) {
		t.Fatal("third request in the same second should be limited")
	}
	// 其它用户不受影响
	if !rl.AllowQPS(8) {
		t.Fatal("another user should not be limited")
	}
}

func TestRateLimiterConcurrency(t *testing.T) {
	rl := NewRateLimiter(0, 2)
	if !rl.Acquire(1) || !rl.Acquire(1) {
		t.Fatal("two concurrent slots should be granted")
	}
	if rl.Acquire(1) {
		t.Fatal("third concurrent should be rejected")
	}
	if !rl.Acquire(2) {
		t.Fatal("other user should not be affected")
	}
	rl.Release(1)
	if !rl.Acquire(1) {
		t.Fatal("slot should be reusable after release")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	if rl.Enabled() {
		t.Fatal("disabled limiter should report disabled")
	}
	if !rl.AllowQPS(1) || !rl.Acquire(1) {
		t.Fatal("disabled limiter passes everything")
	}
	if rl.QPS() != 0 || rl.Concurrency() != 0 {
		t.Fatal("getters should return 0 when disabled")
	}
}
