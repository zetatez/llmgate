// Package puller 定时自动拉取上游模型并创建映射路由。
// 间隔由 settings 的 pull_interval_min 控制（分钟，0 表示禁用），
// 每次循环重新读取，修改配置后下一个周期即生效。
package puller

import (
	"context"
	"encoding/json"
	"log"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/modelpull"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

const defaultIntervalMin = 60

// Puller 定时任务执行体。
type Puller struct {
	a    *app.App
	stop chan struct{}
	pen  *router.Penalizer
	mu   sync.Mutex // 定时循环与手动 SyncNow 互斥，避免并发同步
}

// New 创建定时拉取任务。
func New(a *app.App, pen *router.Penalizer) *Puller {
	return &Puller{a: a, pen: pen, stop: make(chan struct{})}
}

// Run 阻塞执行：启动立即跑一次，之后按设定的间隔循环。
func (p *Puller) Run() {
	for {
		interval := p.intervalMin()
		if interval > 0 {
			p.safeRun("model-sync", func() { p.SyncNow() })
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

// RunBackground 后台任务：冷却渠道探活自愈（每 20s）+ 日志保留清理（每小时）。
func (p *Puller) RunBackground() {
	probeTk := time.NewTicker(20 * time.Second)
	cleanTk := time.NewTicker(time.Hour)
	defer probeTk.Stop()
	defer cleanTk.Stop()

	p.safeRun("probe-once", p.probeOnce)
	p.safeRun("cleanup-once", p.cleanupOnce)
	for {
		select {
		case <-probeTk.C:
			p.safeRun("probe-once", p.probeOnce)
		case <-cleanTk.C:
			p.safeRun("cleanup-once", p.cleanupOnce)
		case <-p.stop:
			return
		}
	}
}

// safeRun 隔离单次后台任务 panic：仅中止当次，绝不让进程因后台任务异常而整体崩溃。
func (p *Puller) safeRun(name string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("puller: panic in %s recovered: %v\n%s", name, rec, debug.Stack())
		}
	}()
	fn()
}

// probeOnce 对"冷却已到期"的渠道做一次轻量探活，成功则恢复 healthy 并清内存熔断。
func (p *Puller) probeOnce() {
	a := p.a
	due, err := store.ListCooldownChannels(a.DB, models.Now())
	if err != nil {
		log.Printf("puller: list cooldown channels: %v", err)
		return
	}
	for _, ch := range due {
		if ch.Enabled != 1 {
			_ = store.SetChannelHealthy(a.DB, ch.ID)
			p.pen.ClearChan(ch.ID)
			continue
		}
		apiKey, err := a.FirstEnabledKey(ch.ID)
		if err != nil {
			continue
		}
		adpt := adapter.Get(ch.Adapter)
		if adpt == nil {
			continue
		}
		// 用真实聊天路径探活（带该渠道任一启用路由的模型）：避免“models 列表通但 chat 挂”
		// 导致冷却被反复误恢复（探活过 20s 一轮，聊天接口持续 EOF/超时则不会恢复）。
		probeModel, _ := store.ProbeRouteModel(a.DB, ch.ID)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(ch.TimeoutMS)*time.Millisecond)
		resp, err := adpt.Do(ctx, adapter.ChannelFrom(ch), apiKey, adapter.HealthRequest(probeModel))
		cancel()
		if err != nil || resp == nil || resp.StatusCode >= 400 {
			continue
		}
		resp.Body.Close()
		_ = store.SetChannelHealthy(a.DB, ch.ID)
		p.pen.ClearChan(ch.ID)
		log.Printf("puller: channel %q 探活成功，已恢复 healthy", ch.Name)
	}
}

// cleanupOnce 按 log_retain_days 清理过期请求日志（0 = 禁用）。
func (p *Puller) cleanupOnce() {
	days := intSetting(p.a, "log_retain_days", 30)
	if days <= 0 {
		return
	}
	cutoff := models.Now() - int64(days)*86400
	n, err := store.DeleteOldLogs(p.a.DB, cutoff)
	if err != nil {
		log.Printf("puller: cleanup logs: %v", err)
		return
	}
	if n > 0 {
		log.Printf("puller: 清理 %d 条过期日志（保留 %d 天）", n, days)
	}
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
