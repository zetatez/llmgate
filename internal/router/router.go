// Package router 负责模型路由选择、Key 池加权与故障转移。
package router

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/secret"
	"llmgate/internal/store"
)

// ErrNoCandidate 表示模型没有任何可用候选（未配置/渠道禁用/全冷却/全熔断）。
var ErrNoCandidate = errors.New("no available channel for this model")

// 熔断退避阶梯：30s → 60s → 15min（之后连续失败保持 15min 封顶）。
var penaltyLadder = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	15 * time.Minute,
}

const quotaPenalty = 15 * time.Minute // 上游额度用尽/欠费等，按最大档冷却（15min）

// Router 持有路由所需依赖。
type Router struct {
	db     *sql.DB
	secret *secret.Manager
	pen    *Penalizer
}

// penaltyState 单个目标（渠道或 Key）的熔断信息。
type penaltyState struct {
	until    time.Time
	attempts int
}

// Penalizer 内存态熔断器（并发安全）。单个，供 Router 与 puller 探活共用。
type Penalizer struct {
	mu  sync.RWMutex
	ch  map[int64]penaltyState // 渠道冷却（网络/5xx/429/额度）
	key map[int64]time.Time    // Key 冷却（401/个Key限流）
}

// NewPenalizer 创建熔断器。
func NewPenalizer() *Penalizer {
	return &Penalizer{ch: map[int64]penaltyState{}, key: map[int64]time.Time{}}
}

// ChPenalized 渠道是否处于熔断中。
func (p *Penalizer) ChPenalized(id int64) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.ch[id]
	return ok && time.Now().Before(s.until)
}

// PenaliseChan 冷却渠道并按阶梯退避：30s → 60s → 15min（此后保持 15min），返回本次冷却时长。
// forced>0 时直接按给定兜底时长。
func (p *Penalizer) PenaliseChan(id int64, forced time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.ch[id]
	s.attempts++
	d := forced
	if d <= 0 {
		idx := s.attempts - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(penaltyLadder) {
			idx = len(penaltyLadder) - 1
		}
		d = penaltyLadder[idx]
	}
	s.until = time.Now().Add(d)
	p.ch[id] = s
	return d
}

// ClearChan 成功响应后清除渠道冷却并重置退避。
func (p *Penalizer) ClearChan(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.ch[id]; ok {
		delete(p.ch, id)
	}
}

// KeyPenalized Key 是否处于熔断中。
func (p *Penalizer) KeyPenalized(id int64) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	t, ok := p.key[id]
	return ok && time.Now().Before(t)
}

func (p *Penalizer) PenalizeKey(id int64, d time.Duration) {
	if id <= 0 {
		return
	}
	if d <= 0 {
		d = 30 * time.Second
	}
	p.mu.Lock()
	p.key[id] = time.Now().Add(d)
	p.mu.Unlock()
}

// Candidate 是模型的一个可用候选（路由 + 渠道）。
type Candidate struct {
	Route   models.ModelRoute
	Channel models.Channel
}

// Result 是转发成功/透传的结果。
type Result struct {
	StatusCode  int
	Body        []byte
	ContentType string
	Channel     models.Channel
	Route       models.ModelRoute
	KeyID       int64
	Retries     int
}

// New 创建 Router，使用外部注入的共享熔断器（与 puller 探活共用）。
func New(a *app.App, pen *Penalizer) *Router {
	if pen == nil {
		pen = NewPenalizer()
	}
	return &Router{db: a.DB, secret: a.Secret, pen: pen}
}

// penalizeChannel 冷却渠道（内存 + DB 持久化 cooldown，重启不丢）。
func (r *Router) penalizeChannel(ch *models.Channel, forced time.Duration) {
	d := r.pen.PenaliseChan(ch.ID, forced)
	_ = store.SetChannelCooldown(r.db, ch.ID, models.Now()+int64(d.Seconds()))
}

// recoverChannel 渠道恢复（清内存熔断 + DB 恢复 healthy）。
func (r *Router) recoverChannel(ch *models.Channel) {
	r.pen.ClearChan(ch.ID)
	_ = store.SetChannelHealthy(r.db, ch.ID)
}

// Candidates 返回某模型可参与路由的候选（启用路由 + 启用渠道 + 非 cooldown）。
func (r *Router) Candidates(ctx context.Context, displayModel string) ([]Candidate, error) {
	rows, err := r.db.Query(`
		SELECT mr.id, mr.display_name, mr.channel_id, mr.upstream_model, mr.priority, mr.weight, mr.enabled,
			ch.id, ch.name, ch.base_url, ch.adapter, ch.priority, ch.weight, ch.timeout_ms, ch.enabled, ch.health_state
		FROM model_routes mr
		JOIN channels ch ON ch.id = mr.channel_id
		WHERE mr.display_name = ? AND mr.enabled = 1 AND ch.enabled = 1 AND ch.health_state != 'cooldown'
		ORDER BY mr.priority ASC, ch.priority ASC, mr.id ASC`, displayModel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(
			&c.Route.ID, &c.Route.DisplayName, &c.Route.ChannelID, &c.Route.UpstreamModel,
			&c.Route.Priority, &c.Route.Weight, &c.Route.Enabled,
			&c.Channel.ID, &c.Channel.Name, &c.Channel.BaseURL, &c.Channel.Adapter,
			&c.Channel.Priority, &c.Channel.Weight, &c.Channel.TimeoutMS, &c.Channel.Enabled, &c.Channel.HealthState,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// candidateLess 有效优先层级比较 = (路由优先级, 渠道优先级)，越小越先消费。
func candidateLess(a, b Candidate) bool {
	if a.Route.Priority != b.Route.Priority {
		return a.Route.Priority < b.Route.Priority
	}
	return a.Channel.Priority < b.Channel.Priority
}

// pickByPriority 在未排除且未熔断的候选中，取最低有效优先层级组内按权重加权随机。
func (r *Router) pickByPriority(cands []Candidate, excludedCh map[int64]bool) *Candidate {
	group := []Candidate{}
	var best *Candidate
	for i := range cands {
		c := cands[i]
		if excludedCh[c.Channel.ID] || r.pen.ChPenalized(c.Channel.ID) {
			continue
		}
		if best == nil || candidateLess(c, *best) {
			best = &c
			group = []Candidate{c}
		} else if best != nil && c.Route.Priority == best.Route.Priority && c.Channel.Priority == best.Channel.Priority {
			group = append(group, c)
		}
	}
	if len(group) == 0 {
		return nil
	}
	// 同层级内加权随机：候选权重 = 路由权重 × 渠道权重
	return weightedRandom(group, func(c Candidate) int {
		return weightOf(c.Route.Weight) * weightOf(c.Channel.Weight)
	})
}

// pickKey 从渠道启用 Key 中加权随机选一个（跳过熔断中的 Key）并解密。
func (r *Router) pickKey(channelID int64, failedKeys map[int64]bool) (*models.ChannelKey, string, error) {
	keys, err := store.ListChannelKeys(r.db, channelID)
	if err != nil {
		return nil, "", err
	}
	var available []*models.ChannelKey
	for _, k := range keys {
		if k.Enabled == 1 && !failedKeys[k.ID] && !r.pen.KeyPenalized(k.ID) {
			available = append(available, k)
		}
	}
	if len(available) == 0 {
		return nil, "", errors.New("channel has no enabled key")
	}
	key := *weightedRandom(available, func(k *models.ChannelKey) int { return weightOf(k.Weight) })
	apiKey, err := r.secret.Decrypt(key.APIKeyEnc)
	if err != nil || apiKey == "" {
		return nil, "", errors.New("decrypt key failed")
	}
	return key, apiKey, nil
}

// isQuotaExceeded 从上游错误体识别"额度/余额用尽"类错误（429/402/403 常见）。
func isQuotaExceeded(body string) bool {
	low := strings.ToLower(body)
	for _, kw := range []string{"quota", "insufficient", "balance", "payment", "billing", "usage limit", "limit reached", "exceeded"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// ForwardChat 将 OpenAI 兼容请求体转发给 displayModel 对应的候选渠道。
// 有效优先级 高→低 依次消费；高优先级被限流/额度用尽/失败时自动下沉到低优先级，
// 并给失败渠道施加内存熔断（指数退避），避免每个请求都重复打失败的渠道。
// 其余 4xx 透传；网络级失败最多尝试 3 次。
func (r *Router) ForwardChat(ctx context.Context, requestBody []byte, displayModel, upstreamPath string) (*Result, error) {
	cands, err := r.Candidates(ctx, displayModel)
	if err != nil {
		return nil, err
	}

	excludedCh := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	networkAttempts := 0
	maxNetworkAttempts := 3

	for {
		cand := r.pickByPriority(cands, excludedCh)
		if cand == nil {
			break
		}
		key, apiKey, err := r.pickKey(cand.Channel.ID, failedKeys)
		if err != nil {
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			excludedCh[cand.Channel.ID] = true
			continue
		}
		adpt := adapter.Get(cand.Channel.Adapter)
		if adpt == nil {
			lastErr = fmt.Errorf("channel %s: adapter %q not registered", cand.Channel.Name, cand.Channel.Adapter)
			excludedCh[cand.Channel.ID] = true
			continue
		}
		resp, err := adpt.Do(ctx, adapter.ChannelFrom(&cand.Channel), apiKey, &adapter.Request{
			Method:  http.MethodPost,
			Path:    upstreamPath,
			Body:    bytes.NewReader(requestBody),
			Headers: map[string]string{},
			Model:   displayModel,
		})
		if err != nil {
			// 网络错误/超时：冷却该渠道（连接级故障对所有 Key 一致）
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			r.penalizeChannel(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			r.penalizeChannel(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		code := resp.StatusCode
		switch {
		case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired || code == http.StatusForbidden:
			// 429 限流 / 402/403 通常是额度、余额、欠费
			bodyStr := truncate(string(body), 300)
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
			if isQuotaExceeded(bodyStr) {
				r.penalizeChannel(&cand.Channel, quotaPenalty) // 额度用尽，15min 内不再重试
			} else if code == http.StatusTooManyRequests {
				r.penalizeChannel(&cand.Channel, 0) // 普通限流，指数退避
			} else {
				// 非额度类 403，可能是个别 Key 的权限问题 → 仅冷却该 Key
				r.pen.PenalizeKey(key.ID, 0)
				failedKeys[key.ID] = true
			}
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusUnauthorized:
			// Key 无效：冷却该 Key，同渠道换 Key 重试
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: unauthorized key", cand.Channel.Name)
			r.pen.PenalizeKey(key.ID, 5*time.Minute)
			failedKeys[key.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusRequestTimeout || code >= http.StatusInternalServerError:
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(body), 300))
			r.penalizeChannel(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusBadRequest || code == http.StatusNotFound:
			// 模型在该渠道不可用 → 故障转移
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(body), 300))
			excludedCh[cand.Channel.ID] = true
			r.recoverChannel(&cand.Channel)
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code >= http.StatusBadRequest:
			// 其余 4xx（422 等）透传，不重试
			r.recoverChannel(&cand.Channel)
			return &Result{StatusCode: code, Body: body, ContentType: resp.Headers["Content-Type"], Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, Retries: networkAttempts}, nil
		default:
			// 成功：清除该渠道熔断并恢复
			r.recoverChannel(&cand.Channel)
			return &Result{StatusCode: http.StatusOK, Body: body, ContentType: resp.Headers["Content-Type"], Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, Retries: networkAttempts}, nil
		}
	}

	if networkAttempts > 0 {
		return nil, lastErr
	}
	return nil, ErrNoCandidate
}

// StreamResult 是流式转发结果：Stream 为已连接的上游 SSE 流；
// Passthrough 非空时表示上游以非 2xx 透传（无流）。
type StreamResult struct {
	StatusCode  int
	ContentType string
	Stream      io.ReadCloser
	Passthrough []byte
	Channel     models.Channel
	Route       models.ModelRoute
	KeyID       int64
	Retries     int
}

// ForwardChatStream 流式转发：在首个字节流出前失败会自动切换到下一候选；
// 一旦流出即绑定该渠道。非 2xx 中 429/402/403/408/5xx/400/404 触发故障转移，其余 4xx 透传。
func (r *Router) ForwardChatStream(ctx context.Context, requestBody []byte, displayModel, upstreamPath string) (*StreamResult, error) {
	cands, err := r.Candidates(ctx, displayModel)
	if err != nil {
		return nil, err
	}
	excludedCh := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	networkAttempts := 0
	maxNetworkAttempts := 3

	for {
		cand := r.pickByPriority(cands, excludedCh)
		if cand == nil {
			break
		}
		key, apiKey, err := r.pickKey(cand.Channel.ID, failedKeys)
		if err != nil {
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			excludedCh[cand.Channel.ID] = true
			continue
		}
		adpt := adapter.Get(cand.Channel.Adapter)
		if adpt == nil {
			lastErr = fmt.Errorf("channel %s: adapter %q not registered", cand.Channel.Name, cand.Channel.Adapter)
			excludedCh[cand.Channel.ID] = true
			continue
		}
		rc, err := adpt.DoStream(ctx, adapter.ChannelFrom(&cand.Channel), apiKey, &adapter.Request{
			Method:  http.MethodPost,
			Path:    upstreamPath,
			Body:    bytes.NewReader(requestBody),
			Headers: map[string]string{},
			Model:   displayModel,
		})
		if err != nil {
			var se *adapter.StatusError
			if !errors.As(err, &se) {
				// 网络/连接失败
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
				r.penalizeChannel(&cand.Channel, 0)
				excludedCh[cand.Channel.ID] = true
				if networkAttempts >= maxNetworkAttempts {
					return nil, lastErr
				}
				continue
			}
			// 上游已响应（非 2xx）
			code := se.StatusCode
			bodyStr := truncate(string(se.Body), 300)
			switch {
			case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired || code == http.StatusForbidden:
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
				if isQuotaExceeded(bodyStr) {
					r.penalizeChannel(&cand.Channel, quotaPenalty)
				} else {
					r.penalizeChannel(&cand.Channel, 0)
				}
				excludedCh[cand.Channel.ID] = true
			case code == http.StatusUnauthorized:
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: unauthorized key", cand.Channel.Name)
				r.pen.PenalizeKey(key.ID, 5*time.Minute)
				failedKeys[key.ID] = true
			case code == http.StatusRequestTimeout || code >= http.StatusInternalServerError || code == http.StatusBadRequest || code == http.StatusNotFound:
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
				r.penalizeChannel(&cand.Channel, 0)
				excludedCh[cand.Channel.ID] = true
			default:
				// 其余 4xx 透传（非流，直接给客户端原始错误体）
				r.recoverChannel(&cand.Channel)
				return &StreamResult{StatusCode: code, ContentType: se.HeaderMap["Content-Type"], Passthrough: se.Body,
					Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, Retries: networkAttempts}, nil
			}
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		// 成功：绑定该渠道并返回 SSE 流
		r.recoverChannel(&cand.Channel)
		return &StreamResult{StatusCode: http.StatusOK, Stream: rc,
			Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, Retries: networkAttempts}, nil
	}

	if networkAttempts > 0 {
		return nil, lastErr
	}
	return nil, ErrNoCandidate
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func weightOf(w int) int {
	if w <= 0 {
		return 1
	}
	return w
}

// weightedRandom 对 items 按 weightOf 权重做一次加权随机选择。
func weightedRandom[T any](items []T, weightOf func(T) int) *T {
	total := 0
	for _, it := range items {
		total += weightOf(it)
	}
	if total <= 0 {
		return &items[0]
	}
	n := rand.Intn(total)
	for i := range items {
		n -= weightOf(items[i])
		if n < 0 {
			return &items[i]
		}
	}
	return &items[len(items)-1]
}
