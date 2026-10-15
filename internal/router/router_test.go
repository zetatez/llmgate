package router

import (
	"testing"
	"time"

	"llmgate/internal/models"
)

func mkCandidate(chID, chPrio, chWeight, routePrio, routeWeight int64) Candidate {
	return Candidate{
		Channel: models.Channel{ID: chID, Priority: int(chPrio), Weight: int(chWeight)},
		Route:   models.ModelRoute{ChannelID: chID, Priority: int(routePrio), Weight: int(routeWeight)},
	}
}

func TestPenaltyLadder(t *testing.T) {
	p := NewPenalizer()
	if d := p.PenaliseChan(1, 0); d != 2*time.Second {
		t.Fatalf("first penalty = %v, want 2s", d)
	}
	if d := p.PenaliseChan(1, 0); d != 4*time.Second {
		t.Fatalf("second penalty = %v, want 4s", d)
	}
	if d := p.PenaliseChan(1, 0); d != 8*time.Second {
		t.Fatalf("third penalty = %v, want 8s", d)
	}
	if d := p.PenaliseChan(1, 0); d != 8*time.Second {
		t.Fatalf("fourth penalty = %v, want 8s (capped)", d)
	}
	if !p.ChPenalized(1) {
		t.Fatal("channel 1 should be penalized")
	}
	p.ClearChan(1)
	if p.ChPenalized(1) {
		t.Fatal("channel 1 should be cleared")
	}
	// 清除后退避从头开始
	if d := p.PenaliseChan(1, 0); d != 2*time.Second {
		t.Fatalf("post-clear penalty = %v, want 2s", d)
	}
}

func TestPenaltyForced(t *testing.T) {
	p := NewPenalizer()
	if d := p.PenaliseChan(9, quotaPenalty); d != quotaPenalty {
		t.Fatalf("forced penalty = %v, want %v", d, quotaPenalty)
	}
}

func TestKeyPenalty(t *testing.T) {
	p := NewPenalizer()
	p.PenalizeKey(1, time.Minute)
	if !p.KeyPenalized(1) {
		t.Fatal("key 1 should be penalized")
	}
	if p.KeyPenalized(2) {
		t.Fatal("key 2 should not be penalized")
	}
}

func TestModelDenied(t *testing.T) {
	p := NewPenalizer()
	p.DenyModel(7, "gpt-x", 10*time.Minute)
	if !p.ModelDenied(7, "gpt-x") {
		t.Fatal("gpt-x on ch7 should be denied")
	}
	if p.ModelDenied(7, "other-model") || p.ModelDenied(8, "gpt-x") {
		t.Fatal("different model/channel should not be denied")
	}
	p.ClearModelDenied(7, "gpt-x")
	if p.ModelDenied(7, "gpt-x") {
		t.Fatal("denied flag should be cleared")
	}
}

func TestModelDeniedKey(t *testing.T) {
	p := NewPenalizer()
	// 未达阈值不拒权
	for i := 0; i < failThreshold-1; i++ {
		p.FailModelKey(11, "gpt-x", time.Minute)
	}
	if p.ModelDeniedKey(11, "gpt-x") {
		t.Fatal("should not deny before threshold")
	}
	p.FailModelKey(11, "gpt-x", time.Minute)
	if !p.ModelDeniedKey(11, "gpt-x") {
		t.Fatal("should deny key's model after threshold")
	}
	// Key 维度隔离：其它 Key / 其它模型不受影响
	if p.ModelDeniedKey(11, "other-model") || p.ModelDeniedKey(12, "gpt-x") {
		t.Fatal("other key/model must not be denied")
	}
	// 与渠道维度互不影响
	if p.ModelDenied(11, "gpt-x") {
		t.Fatal("channel-scoped denial must be independent from key-scoped")
	}
	p.ClearModelDeniedKey(11, "gpt-x")
	if p.ModelDeniedKey(11, "gpt-x") {
		t.Fatal("denied flag should be cleared")
	}
}

func TestCandidateLess(t *testing.T) {
	a := mkCandidate(1, 1, 1, 5, 1) // routePrio 5, chPrio 1
	b := mkCandidate(2, 0, 1, 5, 1) // routePrio 5, chPrio 0
	c := mkCandidate(3, 0, 1, 1, 1) // routePrio 1
	if !candidateLess(c, a) {
		t.Fatal("lower route priority should come first")
	}
	if !candidateLess(b, a) {
		t.Fatal("same route priority, lower channel priority should come first")
	}
	if candidateLess(a, b) {
		t.Fatal("higher channel priority should not win")
	}
}

func TestPickByPriority(t *testing.T) {
	r := &Router{pen: NewPenalizer()}
	// 高优先级(route 0, ch 0) 权重1；低优先级(route 1)
	cands := []Candidate{
		mkCandidate(1, 0, 1, 0, 1), // 最优
		mkCandidate(2, 0, 1, 1, 1), // 次
	}
	pick := r.pickByPriority(cands, map[int64]bool{}, "m")
	if pick == nil || pick.Channel.ID != 1 {
		t.Fatalf("expected channel 1 to be picked, got %+v", pick)
	}
	// 排除渠道 1 → 应选渠道 2
	pick = r.pickByPriority(cands, map[int64]bool{1: true}, "m")
	if pick == nil || pick.Channel.ID != 2 {
		t.Fatalf("expected channel 2 after excluding 1, got %+v", pick)
	}
	// 熔断渠道 1 → 应选渠道 2
	r.pen.PenaliseChan(1, time.Hour)
	pick = r.pickByPriority(cands, map[int64]bool{}, "m")
	if pick == nil || pick.Channel.ID != 2 {
		t.Fatalf("expected channel 2 when channel 1 penalized, got %+v", pick)
	}
	// 全部排除 → nil
	if pick := r.pickByPriority(cands, map[int64]bool{1: true, 2: true}, "m"); pick != nil {
		t.Fatal("expected nil when all excluded")
	}
}

// 所有候选渠道都在冷却（熔断）时，仍应"最后手段"兜底选中最低优先级渠道，
// 不能因为瞬时冷却就误报"无可用渠道"。
func TestPickLastResortOnCooldown(t *testing.T) {
	r := &Router{pen: NewPenalizer()}
	cands := []Candidate{
		mkCandidate(1, 0, 1, 0, 1),
		mkCandidate(2, 1, 1, 1, 1),
	}
	// 两个渠道都熔断冷却
	r.pen.PenaliseChan(1, time.Hour)
	r.pen.PenaliseChan(2, time.Hour)
	pick := r.pickByPriority(cands, map[int64]bool{}, "m")
	if pick == nil {
		t.Fatal("expected last-resort pick when all channels are in cooldown")
	}
	if pick.Channel.ID != 1 {
		t.Fatalf("expected lowest-priority channel 1 as last resort, got %d", pick.Channel.ID)
	}
	// 排除后严格路径仍为空，兜底应落到仅剩的渠道 2
	pick = r.pickByPriority(cands, map[int64]bool{1: true}, "m")
	if pick == nil || pick.Channel.ID != 2 {
		t.Fatalf("expected last-resort on channel 2, got %+v", pick)
	}
	// 模型级 403 拒权不应被兜底绕过
	r.pen.DenyModel(1, "m", time.Hour)
	pick = r.pickByPriority(cands, map[int64]bool{2: true}, "m")
	if pick != nil {
		t.Fatalf("expected nil when only remaining channel is model-denied, got %+v", pick)
	}
}

func TestWeightedRandomRespectsWeights(t *testing.T) {
	// 单元素必然返回它
	items := []int{42}
	if *weightedRandom(items, func(int) int { return 1 }) != 42 {
		t.Fatal("single item failed")
	}
	// 权重全 0 → 返回首个（兜底）
	zero := []string{"a", "b"}
	if *weightedRandom(zero, func(string) int { return 0 }) != "a" {
		t.Fatal("zero-weight fallback failed")
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	cases := map[string]bool{
		`{"error":{"message":"insufficient_quota"}}`: true,
		"you have exceeded your quota":               true,
		"billing issue on your account":              true,
		"your subscription has expired":              true, // 过期/欠费应识别为配额类
		"api key expired, please renew":              true,
		"model not found":                            false,
		"the server had an error":                    false,
		"":                                           false,
	}
	for body, want := range cases {
		if got := isQuotaExceeded(body); got != want {
			t.Fatalf("isQuotaExceeded(%q) = %v, want %v", body, got, want)
		}
	}
}

// 冷却采用"连续失败达阈值才拉黑"，避免一两次偶发失败误伤正常渠道。
func TestFailChanThreshold(t *testing.T) {
	p := NewPenalizer()
	for i := 0; i < failThreshold-1; i++ {
		if d := p.FailChan(1, 0); d != 0 {
			t.Fatalf("below-threshold call #%d should return 0, got %v", i+1, d)
		}
		if p.ChPenalized(1) {
			t.Fatalf("channel must NOT be penalized below threshold (call %d)", i+1)
		}
	}
	// 达阈值才冷却，进入阶梯
	if d := p.FailChan(1, 0); d != 2*time.Second {
		t.Fatalf("first cooldown should be 2s, got %v", d)
	}
	if !p.ChPenalized(1) {
		t.Fatal("channel should be penalized at threshold")
	}
	// forced 时长直接生效（覆盖阶梯）
	p.ClearChan(1)
	for i := 0; i < failThreshold-1; i++ {
		p.FailChan(1, 0)
	}
	if d := p.FailChan(1, quotaPenalty); d != quotaPenalty {
		t.Fatalf("forced cooldown = %v, want %v", d, quotaPenalty)
	}
	if !p.ChPenalized(1) {
		t.Fatal("channel should be penalized with forced quota penalty")
	}
}

// 滑动窗口：相隔较久的孤立失败不应累计触发冷却。
func TestFailChanWindowReset(t *testing.T) {
	p := NewPenalizer()
	p.FailChan(1, 0)
	p.FailChan(1, 0)
	// 手动把最近失败时间拨远，模拟两次失败相隔超过窗口 → 重新计数
	p.mu.Lock()
	s := p.ch[1]
	s.last = time.Now().Add(-2 * failWindow)
	p.ch[1] = s
	p.mu.Unlock()
	if d := p.FailChan(1, 0); d != 0 {
		t.Fatalf("isolated failure after window expiry should not cool, got %v", d)
	}
	if p.ChPenalized(1) {
		t.Fatal("channel should not be penalized after stale failures reset")
	}
}

func TestFailModelThreshold(t *testing.T) {
	p := NewPenalizer()
	for i := 0; i < failThreshold-1; i++ {
		p.FailModel(7, "m", time.Minute)
		if p.ModelDenied(7, "m") {
			t.Fatalf("model must NOT be denied below threshold (call %d)", i+1)
		}
	}
	p.FailModel(7, "m", time.Minute)
	if !p.ModelDenied(7, "m") {
		t.Fatal("model should be denied at threshold")
	}
	if p.ModelDenied(7, "other") {
		t.Fatal("unrelated model should not be denied")
	}
	// 成功恢复即清除
	p.ClearModelDenied(7, "m")
	if p.ModelDenied(7, "m") {
		t.Fatal("model should be cleared after success")
	}
}

// 运行期调优：ApplyTuning 能把默认参数覆盖为 settings 指定值，并实际生效。
func TestApplyTuning(t *testing.T) {
	p := NewPenalizer()
	if p.QuotaPenalty() != quotaPenalty {
		t.Fatalf("default quota penalty = %v, want %v", p.QuotaPenalty(), quotaPenalty)
	}
	// 调低阈值到 2、改窗口与配额冷却
	p.ApplyTuning(Tuning{FailThreshold: 2, QuotaPenalty: 5 * time.Second})
	if p.QuotaPenalty() != 5*time.Second {
		t.Fatalf("quota penalty after tuning = %v, want 5s", p.QuotaPenalty())
	}
	// 阈值 2：第一次失败不冷却，第二次才冷却
	if d := p.FailChan(3, 0); d != 0 {
		t.Fatalf("first fail must not cool after tuning, got %v", d)
	}
	if p.ChPenalized(3) {
		t.Fatal("not penalized after one failure with threshold=2")
	}
	if d := p.FailChan(3, 0); d != 2*time.Second {
		t.Fatalf("second fail should cool (threshold=2), got %v", d)
	}
	if !p.ChPenalized(3) {
		t.Fatal("channel should be penalized after reaching tuned threshold")
	}
	// 零值字段沿用当前值（不改动）
	p.ApplyTuning(Tuning{})
	if p.failThreshold != 2 {
		t.Fatalf("zero tuning must not reset threshold, got %d", p.failThreshold)
	}
}
