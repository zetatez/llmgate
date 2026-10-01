// Package puller 定时自动拉取上游模型并创建映射路由。
// 间隔由 settings 的 pull_interval_min 控制（分钟，0 表示禁用），
// 每次循环重新读取，修改配置后下一个周期即生效。
package puller

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"llmgate/internal/app"
	"llmgate/internal/modelpull"
	"llmgate/internal/store"
)

const defaultIntervalMin = 60

// Puller 定时任务执行体。
type Puller struct {
	a    *app.App
	stop chan struct{}
	mu   sync.Mutex // 定时循环与手动 SyncNow 互斥，避免并发同步
}

// New 创建定时拉取任务。
func New(a *app.App) *Puller {
	return &Puller{a: a, stop: make(chan struct{})}
}

// Run 阻塞执行：启动立即跑一次，之后按设定的间隔循环。
func (p *Puller) Run() {
	for {
		interval := p.intervalMin()
		if interval > 0 {
			p.SyncNow()
		}
		d := time.Duration(interval) * time.Minute
		if d < 0 {
			d = 0
		}
		select {
		case <-time.After(d):
		case <-p.stop:
			return
		}
	}
}

// SyncNow 立即执行一次同步（手动触发或定时循环共用），返回本次执行摘要。
func (p *Puller) SyncNow() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tick()
}

// Stop 停止任务。
func (p *Puller) Stop() { close(p.stop) }

// tick 对所有启用且非冷却的渠道执行一次拉取映射，返回执行摘要。
func (p *Puller) tick() map[string]any {
	a := p.a
	start := time.Now()
	priority := intSetting(a, "pull_default_priority", 0)
	weight := intSetting(a, "pull_default_weight", 1)

	chs, err := store.ListChannels(a.DB)
	if err != nil {
		log.Printf("puller: list channels: %v", err)
		return map[string]any{"error": err.Error()}
	}
	creErr, created, skipped, removed := 0, 0, 0, 0
	for _, ch := range chs {
		if ch.Enabled != 1 || ch.HealthState == "cooldown" {
			continue
		}
		res, err := modelpull.PullChannel(context.Background(), a, ch, modelpull.Options{Priority: priority, Weight: weight})
		if err != nil {
			creErr++
			log.Printf("puller: channel %q: %v", ch.Name, err)
			continue
		}
		created += res.Created
		skipped += res.Skipped
		removed += res.Removed
		if res.Created > 0 || res.Removed > 0 {
			log.Printf("puller: channel %q: 新建 %d 路由, 清理 %d 陈旧路由", ch.Name, res.Created, res.Removed)
		}
	}
	// 记录最近一次执行结果（供后台展示）
	last := map[string]any{
		"at":      time.Now().Unix(),
		"created": created,
		"skipped": skipped,
		"removed": removed,
		"errored": creErr,
		"cost_ms": time.Since(start).Milliseconds(),
	}
	if b, err := json.Marshal(last); err == nil {
		_ = a.SetSetting("model_pull_last", string(b))
	}
	return last
}

func (p *Puller) intervalMin() int {
	return intSetting(p.a, "pull_interval_min", defaultIntervalMin)
}

func intSetting(a *app.App, key string, def int) int {
	v, err := a.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
