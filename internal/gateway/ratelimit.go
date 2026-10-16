package gateway

import (
	"sync"
	"time"
)

// RateLimiter 按用户做网关限流：固定窗口 QPS + 并发在途上限。
// qpsLimit / concLimit 为 0 时该项禁用。
type RateLimiter struct {
	mu        sync.Mutex
	qpsLimit  int
	concLimit int
	window    map[int64]*qpsState
	inflight  map[int64]int
}

type qpsState struct {
	sec   int64
	count int
}

// NewRateLimiter 创建限流器；qps=0 关闭 QPS 限流，conc=0 关闭并发限流。
func NewRateLimiter(qps, conc int) *RateLimiter {
	return &RateLimiter{
		qpsLimit:  qps,
		concLimit: conc,
		window:    map[int64]*qpsState{},
		inflight:  map[int64]int{},
	}
}

// Enabled 是否启用了任一维度限流。
func (r *RateLimiter) Enabled() bool {
	return r != nil && (r.qpsLimit > 0 || r.concLimit > 0)
}

// QPS 返回 QPS 上限（0=禁用）。
func (r *RateLimiter) QPS() int {
	if r == nil {
		return 0
	}
	return r.qpsLimit
}

// Concurrency 返回并发上限（0=禁用）。
func (r *RateLimiter) Concurrency() int {
	if r == nil {
		return 0
	}
	return r.concLimit
}

// AllowQPS 固定秒窗口计数：该秒内超过 qpsLimit 次返回 false。
func (r *RateLimiter) AllowQPS(id int64) bool {
	if r == nil || r.qpsLimit <= 0 {
		return true
	}
	now := time.Now().Unix()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.window[id]
	if s == nil {
		s = &qpsState{}
		r.window[id] = s
	}
	if s.sec != now {
		s.sec = now
		s.count = 0
	}
	s.count++
	return s.count <= r.qpsLimit
}

// Acquire 抢占一个并发额度；超过 concLimit 返回 false。
func (r *RateLimiter) Acquire(id int64) bool {
	if r == nil || r.concLimit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := r.inflight[id]
	if cur+1 > r.concLimit {
		return false
	}
	r.inflight[id] = cur + 1
	return true
}

// Release 释放一个并发额度。
func (r *RateLimiter) Release(id int64) {
	if r == nil || r.concLimit <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur := r.inflight[id]; cur <= 1 {
		delete(r.inflight, id)
	} else {
		r.inflight[id] = cur - 1
	}
}
