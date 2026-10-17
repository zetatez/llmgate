// Package router 负责模型路由选择、Key 池加权与故障转移。
package router

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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

// ErrNoCandidate 表示该模型未配置任何启用路由（"永久"不可用 → 404，客户端不应重试）。
var ErrNoCandidate = errors.New("no available channel for this model")

// ErrChannelBusy 表示该模型有路由但所有渠道当前都在冷却/熔断/拒权中
// （临时性 → 503+Retry-After，客户端可自动退避重试实现无缝续接）。
var ErrChannelBusy = errors.New("all channels are temporarily cooling down for this model, please retry shortly")

// ErrNetworkExhausted 表示所有候选渠道都因纯网络故障（未收到任何 HTTP 响应）失败：
// 同样是临时性 → 503+Retry-After，让客户端退避重试而不是立刻撞同一坏渠道。
var ErrNetworkExhausted = errors.New("all channels failed due to network errors, please retry shortly")

// protoUnsupportedMarker 上游 400 响应体中标识"模型不支持当前调用协议"的字段类型。
// 属于确定性、模型级持久错误（如模型只支持 Responses/Claude 协议却被发到 chat/completions）。
const protoUnsupportedMarker = "ModelProtocolUnsupported"

// 熔断退避阶梯：2s → 4s → 8s（之后连续失败保持 8s 封顶）。
var penaltyLadder = []time.Duration{
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}

// quotaPenalty 上游额度用尽/欠费/过期的冷却时长。
// 比普通限流更久：这类问题不易在 8s 内自愈，且避免探活每 tick 去重打一个已停付/欠费账户。
const quotaPenalty = 30 * time.Second

// keyRatePenalty 普通限流（429 非额度）的 Key 级冷却时长。
// 按 Key 隔离：某用户 Key 被限流不会冷却整条渠道、不影响同渠道其它用户。
const keyRatePenalty = 20 * time.Second

// failThreshold / failWindow：冷却采用"连续失败才拉黑"的渐进策略。
// 仅在 failWindow 滑动窗口内连续失败达到 failThreshold 次，才真正把渠道/模型拉进冷却，
// 避免一两次偶发失败（瞬时抖动、个别限流、残留缓存/文案误判）就把正常渠道/模型误判拉黑。
//
// 取值权衡：窗口 30s 内连续失败 6 次才冷却——对突发/间歇性故障足够鲁棒，
// 普通限流瞬时命中一两下不会误伤；真正持续故障的渠道在 30s 内基本都会到 6 次触发冷却。
const (
	failThreshold = 6
	failWindow    = 30 * time.Second
)

// Router 持有路由所需依赖。
type Router struct {
	db     *sql.DB
	secret *secret.Manager
	pen    *Penalizer
	userID int64 // 请求方用户；0 = 管理员/全局（仅使用 user_id=0 的 Key）
}

// SetUser 指定本次路由的请求方用户（按归属过滤可用 Key）。返回自身便于链式调用。
func (r *Router) SetUser(userID int64) *Router {
	r.userID = userID
	return r
}

// penaltyState 单个渠道的熔断信息。
// fails 记录滑动窗口内连续失败次数；attempts 为实际冷却的退避阶梯推进记录。
type penaltyState struct {
	until    time.Time
	fails    int
	last     time.Time
	attempts int
}

// modelState 单个模型在单渠道上的拒权熔断信息。
type modelState struct {
	until time.Time
	fails int
	last  time.Time
}

// Penalizer 内存态熔断器（并发安全）。单个，供 Router 与 puller 探活共用。
// 熔断阈值/窗口/配额冷却均可在运行期通过 settings 覆盖（见 SetTuning）。
type Penalizer struct {
	mu            sync.RWMutex
	ch            map[int64]penaltyState           // 渠道冷却（网络/5xx/超时等连接级故障）
	key           map[int64]time.Time              // Key 冷却（401/限流/额度等账号级故障）
	mdl           map[int64]map[string]*modelState // 模型级拒权（渠道维度：协议不支持等）
	mdlKey        map[int64]map[string]*modelState // 模型级拒权（Key 维度：403 access_denied 等，按 Key 隔离避免跨用户串扰）
	failThreshold int                              // 连续失败阈值
	failWindow    time.Duration                    // 连续失败计数的滑动窗口
	quotaPenalty  time.Duration                    // 额度/欠费/过期冷却时长
}

// NewPenalizer 创建熔断器（使用默认熔断参数）。
func NewPenalizer() *Penalizer {
	return &Penalizer{
		ch:            map[int64]penaltyState{},
		key:           map[int64]time.Time{},
		mdl:           map[int64]map[string]*modelState{},
		mdlKey:        map[int64]map[string]*modelState{},
		failThreshold: failThreshold,
		failWindow:    failWindow,
		quotaPenalty:  quotaPenalty,
	}
}

// Tuning 熔断参数的运行期可调项。零值表示"沿用当前值"。
type Tuning struct {
	FailThreshold int
	FailWindow    time.Duration
	QuotaPenalty  time.Duration
}

// DefaultTuning 返回熔断参数的工厂默认值（供 main 读取 settings 前取基线）。
func DefaultTuning() Tuning {
	return Tuning{FailThreshold: failThreshold, FailWindow: failWindow, QuotaPenalty: quotaPenalty}
}

// ApplyTuning 用给定值覆盖熔断参数；各字段若为正才生效，零值保持当前不变。
func (p *Penalizer) ApplyTuning(t Tuning) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.FailThreshold > 0 {
		p.failThreshold = t.FailThreshold
	}
	if t.FailWindow > 0 {
		p.failWindow = t.FailWindow
	}
	if t.QuotaPenalty > 0 {
		p.quotaPenalty = t.QuotaPenalty
	}
}

// QuotaPenalty 返回额度/欠费/过期的冷却时长（调用方转发时用于冷却判定）。
func (p *Penalizer) QuotaPenalty() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.quotaPenalty
}

// ---- 模型级熔断 ----
// 分两个维度：
//   - 渠道维度（mdl）：协议不支持等"渠道/模型"确定性错误，对所有用户一致；
//   - Key 维度（mdlKey）：403 access_denied 等"账号级"错误，按 Key 隔离，
//     避免某用户 Key 的问题把该模型对整条渠道的所有用户一起拉黑（跨用户串扰）。

func modelDeniedLocked(m map[int64]map[string]*modelState, id int64, model string) bool {
	if byModel, ok := m[id]; ok {
		if st, ok := byModel[model]; ok && time.Now().Before(st.until) {
			return true
		}
	}
	return false
}

func failModelLocked(m map[int64]map[string]*modelState, id int64, model string, d, window time.Duration, threshold int) {
	if d <= 0 {
		d = 10 * time.Minute
	}
	byModel := m[id]
	if byModel == nil {
		byModel = map[string]*modelState{}
		m[id] = byModel
	}
	st := byModel[model]
	if st == nil {
		st = &modelState{}
		byModel[model] = st
	}
	now := time.Now()
	if st.last.IsZero() || now.Sub(st.last) > window {
		st.fails = 0
	}
	st.fails++
	st.last = now
	if st.fails < threshold {
		st.until = time.Time{}
		return
	}
	st.until = now.Add(d)
}

func denyModelLocked(m map[int64]map[string]*modelState, id int64, model string, d time.Duration) {
	byModel := m[id]
	if byModel == nil {
		byModel = map[string]*modelState{}
		m[id] = byModel
	}
	st := byModel[model]
	if st == nil {
		st = &modelState{}
		byModel[model] = st
	}
	st.until = time.Now().Add(d)
}

func clearModelLocked(m map[int64]map[string]*modelState, id int64, model string) {
	if byModel, ok := m[id]; ok {
		delete(byModel, model)
		if len(byModel) == 0 {
			delete(m, id)
		}
	}
}

// ModelDenied 某模型在指定渠道是否被标记为"无权访问"（渠道维度）。
func (p *Penalizer) ModelDenied(ch int64, model string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return modelDeniedLocked(p.mdl, ch, model)
}

// FailModel 记录一次渠道维度模型失败；达连续失败阈值才真正拒权（避免误判）。
func (p *Penalizer) FailModel(ch int64, model string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	failModelLocked(p.mdl, ch, model, d, p.failWindow, p.failThreshold)
}

// DenyModel 直接标记模型在渠道上拒权（确定性/高置信场景，如协议不支持）。不做阈值计数。
func (p *Penalizer) DenyModel(ch int64, model string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	denyModelLocked(p.mdl, ch, model, d)
}

// ClearModelDenied 成功调用后清除该模型在渠道上的拒权标记。
func (p *Penalizer) ClearModelDenied(ch int64, model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	clearModelLocked(p.mdl, ch, model)
}

// ModelDeniedKey 某模型在"某 Key"上是否被拒权（Key 维度）。
func (p *Penalizer) ModelDeniedKey(keyID int64, model string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return modelDeniedLocked(p.mdlKey, keyID, model)
}

// FailModelKey 记录一次"Key + 模型"失败；达连续失败阈值才真正拒权（避免误判）。
func (p *Penalizer) FailModelKey(keyID int64, model string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	failModelLocked(p.mdlKey, keyID, model, d, p.failWindow, p.failThreshold)
}

// ClearModelDeniedKey 成功调用后清除该 Key 在该模型上的拒权标记。
func (p *Penalizer) ClearModelDeniedKey(keyID int64, model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	clearModelLocked(p.mdlKey, keyID, model)
}

// ChPenalized 渠道是否处于熔断中。
func (p *Penalizer) ChPenalized(id int64) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.ch[id]
	return ok && time.Now().Before(s.until)
}

// PenaliseChan 直接冷却渠道（高置信场景/探活失败退避）。按阶梯推进：2s → 4s → 8s（封顶 8s）。
// forced>0 时直接按给定兜底时长。ClearChan 后阶梯从头开始。
func (p *Penalizer) PenaliseChan(id int64, forced time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.ch[id]
	d := forced
	if d <= 0 {
		idx := s.attempts
		if idx < 0 {
			idx = 0
		}
		if idx >= len(penaltyLadder) {
			idx = len(penaltyLadder) - 1
		}
		d = penaltyLadder[idx]
	}
	s.attempts++
	s.fails++
	s.last = time.Now()
	s.until = time.Now().Add(d)
	p.ch[id] = s
	return d
}

// FailChan 记录一次渠道失败：达连续失败阈值才真正冷却（按 forced 或指数阶梯）。
// 未达阈值时不拉黑（返回 0），仅计入滑动窗口；本次请求仍会由调用方的 excludedCh 排除做故障转移。
func (p *Penalizer) FailChan(id int64, forced time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.ch[id]
	now := time.Now()
	if s.last.IsZero() || now.Sub(s.last) > p.failWindow {
		s.fails = 0
		s.attempts = 0
	}
	s.fails++
	s.last = now
	if s.fails < p.failThreshold {
		s.until = time.Time{}
		p.ch[id] = s
		return 0
	}
	d := forced
	if d <= 0 {
		if s.attempts >= len(penaltyLadder) {
			d = penaltyLadder[len(penaltyLadder)-1]
		} else {
			d = penaltyLadder[s.attempts]
		}
		s.attempts++
	}
	s.until = now.Add(d)
	s.fails = 0 // 拉黑后清空计数，下一段冷却按阶梯继续推进
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

// channelHeaders 解析渠道配置的额外请求头（JSON map），供附加到上游请求。
// channelHeaderBlock 渠道 extra_headers 中禁止覆盖的敏感头（即使配置也忽略）。
var channelHeaderBlock = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"host":                true,
	"cookie":              true,
	"set-cookie":          true,
	"content-length":      true,
	"transfer-encoding":   true,
	"connection":          true,
}

func channelHeaders(ch *models.Channel) map[string]string {
	out := map[string]string{}
	if ch == nil || ch.ExtraHeaders == "" {
		return out
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(ch.ExtraHeaders), &m); err != nil {
		return out
	}
	for k, v := range m {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" || v == "" || channelHeaderBlock[lk] {
			continue
		}
		out[lk] = v
	}
	return out
}

// mergeHeaders 合并客户端透传头与渠道配置头（渠道配置优先）。
func mergeHeaders(pass, chHdr map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range pass {
		out[k] = v
	}
	for k, v := range chHdr {
		out[k] = v
	}
	return out
}

// rewriteModel 将 OpenAI 请求体中的 "model" 字段改写为目标上游模型名（保真：其余字段原样保留）。
// 别名/厂商前缀路由下，上游只认自己的模型 id（如 opencode / deepseek 只认裸名）。
func rewriteModel(body []byte, upstreamModel string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if b, err := json.Marshal(upstreamModel); err == nil {
		m["model"] = b
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// Result 是转发成功/透传的结果。
type Result struct {
	StatusCode  int
	Body        []byte
	ContentType string
	Channel     models.Channel
	Route       models.ModelRoute
	KeyID       int64
	KeyOwnerID  int64
	Retries     int
}

// New 创建 Router，使用外部注入的共享熔断器（与 puller 探活共用）。
func New(a *app.App, pen *Penalizer) *Router {
	if pen == nil {
		pen = NewPenalizer()
	}
	return &Router{db: a.DB, secret: a.Secret, pen: pen}
}

// recordChannelFail 记录一次渠道失败：达连续失败阈值才真正冷却（内存 + DB 持久化）。
// 返回是否真正进入冷却。未达阈值时不拉黑渠道，仅计入滑动窗口；本次请求仍已被 excludedCh
// 排除做故障转移，避免一两次偶发失败就把正常渠道误判拉黑。
func (r *Router) recordChannelFail(ch *models.Channel, forced time.Duration) bool {
	d := r.pen.FailChan(ch.ID, forced)
	if d > 0 {
		_ = store.SetChannelCooldown(r.db, ch.ID, models.Now()+int64(d.Seconds()))
		return true
	}
	return false
}

// recoverChannel 渠道恢复（清内存熔断 + DB 恢复 healthy）。
// 成功即复位内存态连续失败计数/退避；仅当确实处于冷却（内存或 DB）时才写库，
// 避免每次成功请求都产生一次无谓 UPDATE（单连接 SQLite 下尤为重要）。
func (r *Router) recoverChannel(ch *models.Channel) {
	penalized := r.pen.ChPenalized(ch.ID)
	r.pen.ClearChan(ch.ID)
	if penalized || ch.HealthState != "healthy" {
		_ = store.SetChannelHealthy(r.db, ch.ID)
	}
}

// Candidates 返回某模型可参与路由的候选（启用路由 + 启用渠道）。
// 多租户 BYOK：仅保留"请求用户在该渠道至少有一个启用 Key"的候选，
// 避免对用户没有任何可用 Key 的渠道做无谓尝试（用户对该模型表现为 404）。
// 注意：不在此处排除冷却渠道——冷却/拒权由 pickByPriority 在"最后手段"阶段决定，
// 以免模型的所有候选渠道都在冷却时误报"无可用渠道"。
func (r *Router) Candidates(ctx context.Context, displayModel string) ([]Candidate, error) {
	rows, err := r.db.Query(`
		SELECT mr.id, mr.display_name, mr.channel_id, mr.upstream_model, mr.priority, mr.weight, mr.enabled,
			mr.price_input, mr.price_output,
			ch.id, ch.name, ch.base_url, ch.adapter, ch.priority, ch.weight, ch.timeout_ms, ch.enabled, ch.health_state, ch.extra_headers
		FROM model_routes mr
		JOIN channels ch ON ch.id = mr.channel_id
		WHERE mr.display_name = ? AND mr.enabled = 1 AND ch.enabled = 1
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
			&c.Route.Priority, &c.Route.Weight, &c.Route.Enabled, &c.Route.PriceInput, &c.Route.PriceOutput,
			&c.Channel.ID, &c.Channel.Name, &c.Channel.BaseURL, &c.Channel.Adapter,
			&c.Channel.Priority, &c.Channel.Weight, &c.Channel.TimeoutMS, &c.Channel.Enabled, &c.Channel.HealthState,
			&c.Channel.ExtraHeaders,
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

// pickByPriority 在未排除/未拒绝/未熔断候选中，取最低有效优先层级组内按权重加权随机。
// 严谨筛选无可选时，"最后手段"兜底：忽略渠道冷却仍去尝试最低层级候选（可用性优先），
// 但始终尊重 excludedCh 与本模型的 403 拒权标记，避免明知会被打回 403 仍去撞。
func (r *Router) pickByPriority(cands []Candidate, excludedCh map[int64]bool, displayModel string) *Candidate {
	c := r.pickBest(cands, excludedCh, displayModel, false)
	if c != nil {
		return c
	}
	return r.pickBest(cands, excludedCh, displayModel, true)
}

// pickBest 在内层筛选后的候选中取最低有效优先级组内加权随机；allowCooldown=true 时忽略渠道冷却。
func (r *Router) pickBest(cands []Candidate, excludedCh map[int64]bool, displayModel string, allowCooldown bool) *Candidate {
	group := []Candidate{}
	var best *Candidate
	for i := range cands {
		c := cands[i]
		if excludedCh[c.Channel.ID] || r.pen.ModelDenied(c.Channel.ID, displayModel) {
			continue
		}
		if !allowCooldown && r.pen.ChPenalized(c.Channel.ID) {
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
// 共享池：所有用户的 Key 都参与路由；选中 Key 的归属用户作为"贡献者"由调用方记账。
func (r *Router) pickKey(channelID int64, displayModel string, failedKeys map[int64]bool) (*models.ChannelKey, string, error) {
	keys, err := store.ListChannelKeys(r.db, channelID)
	if err != nil {
		return nil, "", err
	}
	var available []*models.ChannelKey
	for _, k := range keys {
		if k.Enabled == 1 && !failedKeys[k.ID] && !r.pen.KeyPenalized(k.ID) && !r.pen.ModelDeniedKey(k.ID, displayModel) {
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
	for _, kw := range []string{"quota", "insufficient", "balance", "payment", "billing", "usage limit",
		"limit reached", "exceeded", "expired", "expiration", "past due", "credit", "overdue", "subscription"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// ForwardChat 将 OpenAI 兼容请求体转发给 displayModel 对应的候选渠道。
// 有效优先级 高→低 依次消费；高优先级被限流/额度用尽/失败时自动下沉到低优先级，
// 并给失败渠道施加内存熔断（指数退避），避免每个请求都重复打失败的渠道。
// 其余 4xx 透传；网络级失败/上游错误最多尝试 5 次。
// maxUpstreamResp 非流式上游响应体上限（防异常大响应占内存）。
const maxUpstreamResp = 64 << 20 // 64MB

func (r *Router) ForwardChat(ctx context.Context, requestBody []byte, displayModel, upstreamPath string, passHeaders map[string]string) (*Result, error) {
	cands, err := r.Candidates(ctx, displayModel)
	if err != nil {
		return nil, err
	}

	excludedCh := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	lastNetworkErr := false // 最近一次失败是否为纯网络故障（未收到 HTTP 响应）
	// 模型级"协议不支持"错误：确定性、永久性，透传给客户端停止其无谓重试
	var protoBody []byte
	var protoCtype string
	var protoChannel models.Channel
	var protoRoute models.ModelRoute
	var protoKeyID int64
	var protoKeyOwnerID int64
	protoProvided := false
	networkAttempts := 0
	maxNetworkAttempts := 5

	for {
		// 客户端已断开（上下文取消）：不要再消耗失败转移预算去试上游。
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("request cancelled: %w", err)
		}
		cand := r.pickByPriority(cands, excludedCh, displayModel)
		if cand == nil {
			break
		}
		key, apiKey, err := r.pickKey(cand.Channel.ID, displayModel, failedKeys)
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
		resBody := rewriteModel(requestBody, cand.Route.UpstreamModel)
		resp, err := adpt.Do(ctx, adapter.ChannelFrom(&cand.Channel), apiKey, &adapter.Request{
			Method:  http.MethodPost,
			Path:    upstreamPath,
			Body:    bytes.NewReader(resBody),
			Headers: mergeHeaders(passHeaders, channelHeaders(&cand.Channel)),
			Model:   cand.Route.UpstreamModel,
		})
		if err != nil {
			// 网络错误/超时：冷却该渠道（连接级故障对所有 Key 一致）
			networkAttempts++
			lastNetworkErr = true
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamResp+1))
		resp.Body.Close()
		if err != nil {
			// 响应体读取中断：连接在响应中途被掐断，同样视为纯网络故障
			networkAttempts++
			lastNetworkErr = true
			lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		if len(body) > maxUpstreamResp {
			networkAttempts++
			lastErr = fmt.Errorf("channel %s: upstream response too large", cand.Channel.Name)
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		code := resp.StatusCode
		switch {
		case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired || code == http.StatusForbidden:
			// 429 限流 / 402/403 额度/余额/权限：均为"该 Key 所属账号"的问题。
			// 按 Key 隔离冷却（不冷却整条渠道），避免某用户 Key 的问题拖累同渠道其它用户；
			// 标记该 Key 失败后继续尝试同渠道的其它 Key。
			bodyStr := truncate(string(body), 300)
			networkAttempts++
			lastNetworkErr = false // 上游已响应：非纯网络故障
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
			if isQuotaExceeded(bodyStr) {
				r.pen.PenalizeKey(key.ID, r.pen.QuotaPenalty()) // 额度/欠费/过期：Key 级长冷却
			} else if code == http.StatusTooManyRequests {
				r.pen.PenalizeKey(key.ID, keyRatePenalty) // 普通限流：Key 级短冷却
			} else {
				// 非额度类 403/402（如 access_denied）：该 Key 无此模型权限 → Key 级模型拒权
				r.pen.FailModelKey(key.ID, displayModel, 10*time.Minute)
			}
			failedKeys[key.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusUnauthorized:
			// Key 无效：冷却该 Key，同渠道换 Key 重试
			networkAttempts++
			lastNetworkErr = false // 上游已响应 401：非纯网络故障
			lastErr = fmt.Errorf("channel %s: unauthorized key", cand.Channel.Name)
			r.pen.PenalizeKey(key.ID, 5*time.Minute)
			failedKeys[key.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusRequestTimeout || code >= http.StatusInternalServerError:
			networkAttempts++
			lastNetworkErr = false // 上游已响应 5xx：非纯网络故障
			lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, truncate(string(body), 300))
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		case code == http.StatusBadRequest && bytes.Contains(body, []byte(protoUnsupportedMarker)):
			// 模型不支持当前调用协议（如只支持 Responses/Claude 而走 chat/completions）：
			// 确定性、模型级持久错误。标记拒权以快速跳过，不误伤整条渠道；记录体供透传。
			networkAttempts++
			lastNetworkErr = false
			r.pen.DenyModel(cand.Channel.ID, displayModel, 30*time.Minute)
			r.recoverChannel(&cand.Channel)
			excludedCh[cand.Channel.ID] = true
			lastErr = fmt.Errorf("channel %s: model protocol unsupported (upstream 400)", cand.Channel.Name)
			protoBody = body
			protoCtype = resp.Headers["Content-Type"]
			protoChannel = cand.Channel
			protoRoute = cand.Route
			protoKeyID = key.ID
			protoKeyOwnerID = key.UserID
			protoProvided = true
			continue
		case code == http.StatusBadRequest || code == http.StatusNotFound:
			// 模型在该渠道不可用 → 故障转移
			networkAttempts++
			lastNetworkErr = false // 上游已响应：非纯网络故障
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
			r.pen.ClearModelDenied(cand.Channel.ID, displayModel)
			r.pen.ClearModelDeniedKey(key.ID, displayModel)
			return &Result{StatusCode: code, Body: body, ContentType: resp.Headers["Content-Type"], Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, KeyOwnerID: key.UserID, Retries: networkAttempts}, nil
		default:
			// 成功（2xx）或上游 3xx：清除该渠道熔断并恢复，透传真实状态码
			r.recoverChannel(&cand.Channel)
			r.pen.ClearModelDenied(cand.Channel.ID, displayModel)
			r.pen.ClearModelDeniedKey(key.ID, displayModel)
			return &Result{StatusCode: resp.StatusCode, Body: body, ContentType: resp.Headers["Content-Type"], Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, KeyOwnerID: key.UserID, Retries: networkAttempts}, nil
		}
	}

	if protoProvided {
		// 确定性协议错误：把上游 400 原样透传，客户端看到明确原因即停止无谓重试。
		return &Result{StatusCode: http.StatusBadRequest, Body: protoBody, ContentType: protoCtype,
			Channel: protoChannel, Route: protoRoute, KeyID: protoKeyID, KeyOwnerID: protoKeyOwnerID, Retries: networkAttempts}, nil
	}
	if networkAttempts > 0 {
		if lastNetworkErr {
			// 全部候选都死于纯网络故障（无任何 HTTP 响应）：临时性，客户端应退避重试。
			return nil, ErrNetworkExhausted
		}
		return nil, lastErr
	}
	if len(cands) == 0 {
		return nil, ErrNoCandidate
	}
	// 有候选但全被拒权/冷却排除：临时性，客户端应退避重试。
	return nil, ErrChannelBusy
}

// StreamResult 是流式转发结果：Stream 为已连接的上游 SSE 流；
// Passthrough 非空时表示上游以非 2xx 透传（无流）。
type StreamResult struct {
	StatusCode  int
	ContentType string
	Stream      io.ReadCloser
	// Buffered 是网关在确认上游稳定前预读的首批 SSE 事件（可能含首个 data 事件），
	// 调用方必须先写给客户端再继续读 Stream。
	Buffered    []byte
	Passthrough []byte
	Channel     models.Channel
	Route       models.ModelRoute
	KeyID       int64
	KeyOwnerID  int64
	Retries     int
}

// ForwardChatStream 流式转发：在上游稳定送出首个 SSE data 事件前失败会自动切换下一候选；
// 一旦首个事件交付即绑定该渠道。非 2xx 中 429/402/403/408/5xx/400/404 触发故障转移，其余 4xx 透传。
func (r *Router) ForwardChatStream(ctx context.Context, requestBody []byte, displayModel, upstreamPath string, passHeaders map[string]string) (*StreamResult, error) {
	cands, err := r.Candidates(ctx, displayModel)
	if err != nil {
		return nil, err
	}
	excludedCh := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	lastNetworkErr := false // 最近一次失败是否为纯网络故障（未收到 HTTP 响应）
	// 模型级"协议不支持"错误：确定性、永久性，透传给客户端停止其无谓重试
	var protoBody []byte
	var protoCtype string
	var protoChannel models.Channel
	var protoRoute models.ModelRoute
	var protoKeyID int64
	var protoKeyOwnerID int64
	protoProvided := false
	networkAttempts := 0
	maxNetworkAttempts := 5

	for {
		// 客户端已断开：停止消耗失败转移预算。
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("request cancelled: %w", err)
		}
		cand := r.pickByPriority(cands, excludedCh, displayModel)
		if cand == nil {
			break
		}
		key, apiKey, err := r.pickKey(cand.Channel.ID, displayModel, failedKeys)
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
			Body:    bytes.NewReader(rewriteModel(requestBody, cand.Route.UpstreamModel)),
			Headers: mergeHeaders(passHeaders, channelHeaders(&cand.Channel)),
			Model:   cand.Route.UpstreamModel,
		})
		if err != nil {
			var se *adapter.StatusError
			if !errors.As(err, &se) {
				// 网络/连接失败
				networkAttempts++
				lastNetworkErr = true
				lastErr = fmt.Errorf("channel %s: %v", cand.Channel.Name, err)
				r.recordChannelFail(&cand.Channel, 0)
				excludedCh[cand.Channel.ID] = true
				if networkAttempts >= maxNetworkAttempts {
					return nil, lastErr
				}
				continue
			}
			// 上游已响应（非 2xx）
			code := se.StatusCode
			bodyStr := truncate(string(se.Body), 300)
			lastNetworkErr = false // 上游已响应：非纯网络故障
			switch {
			case code == http.StatusBadRequest && bytes.Contains(se.Body, []byte(protoUnsupportedMarker)):
				// 模型不支持当前调用协议：确定性模型级错误。标记拒权快速跳过、不误伤渠道，记录体供透传。
				networkAttempts++
				r.pen.DenyModel(cand.Channel.ID, displayModel, 30*time.Minute)
				r.recoverChannel(&cand.Channel)
				excludedCh[cand.Channel.ID] = true
				lastErr = fmt.Errorf("channel %s: model protocol unsupported (upstream 400)", cand.Channel.Name)
				protoBody = se.Body
				protoCtype = se.HeaderMap["Content-Type"]
				protoChannel = cand.Channel
				protoRoute = cand.Route
				protoKeyID = key.ID
				protoKeyOwnerID = key.UserID
				protoProvided = true
				continue
			case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired || code == http.StatusForbidden:
				// 429/402/403：账号级问题，按 Key 隔离冷却（不冷却整条渠道），并标记该 Key 失败
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
				if isQuotaExceeded(bodyStr) {
					r.pen.PenalizeKey(key.ID, r.pen.QuotaPenalty())
				} else if code == http.StatusTooManyRequests {
					r.pen.PenalizeKey(key.ID, keyRatePenalty)
				} else {
					// 非额度类 403/402（access_denied 等）：该 Key 无此模型权限 → Key 级模型拒权
					r.pen.FailModelKey(key.ID, displayModel, 10*time.Minute)
				}
				failedKeys[key.ID] = true
			case code == http.StatusUnauthorized:
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: unauthorized key", cand.Channel.Name)
				r.pen.PenalizeKey(key.ID, 5*time.Minute)
				failedKeys[key.ID] = true
			case code == http.StatusRequestTimeout || code >= http.StatusInternalServerError:
				// 连接级/服务端故障：冷却整条渠道，指数退避
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
				r.recordChannelFail(&cand.Channel, 0)
				excludedCh[cand.Channel.ID] = true
			case code == http.StatusBadRequest || code == http.StatusNotFound:
				// 400/404 多为"该模型在此渠道不可用/不认此协议"，属模型级问题：
				// 与非流式一致，仅排除故障转移，不冷却整条渠道（避免拖累其上其它模型）
				networkAttempts++
				lastErr = fmt.Errorf("channel %s: upstream status %d: %s", cand.Channel.Name, code, bodyStr)
				r.recoverChannel(&cand.Channel)
				excludedCh[cand.Channel.ID] = true
			default:
				// 其余 4xx 透传（非流，直接给客户端原始错误体）
				r.recoverChannel(&cand.Channel)
				r.pen.ClearModelDenied(cand.Channel.ID, displayModel)
				r.pen.ClearModelDeniedKey(key.ID, displayModel)
				return &StreamResult{StatusCode: code, ContentType: se.HeaderMap["Content-Type"], Passthrough: se.Body,
					Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, KeyOwnerID: key.UserID, Retries: networkAttempts}, nil
			}
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		// 已连上上游（HTTP 200），但还不能立刻交付给客户端：
		// 先缓冲直到出现第一个 SSE data 事件。若在首个事件前断流（EOF/网络中断），
		// 说明这条渠道"连接即死"，对客户端透明地换下一候选/Key 重试，避免空流/断流暴露给用户。
		firstBuf := make([]byte, 0, 8<<10)
		rbuf := make([]byte, 16<<10)
		firstErr := error(nil)
	bufferLoop:
		for {
			n, rerr := rc.Read(rbuf)
			if n > 0 {
				firstBuf = append(firstBuf, rbuf[:n]...)
				if bytes.Contains(firstBuf, []byte("data:")) || len(firstBuf) >= 512<<10 {
					break bufferLoop
				}
			}
			if rerr != nil {
				firstErr = rerr
				break bufferLoop
			}
		}
		if firstErr != nil && !bytes.Contains(firstBuf, []byte("data:")) {
			// 首事件前中断：视为未交付，走故障转移（连接已建但流即死 → 纯网络故障）
			rc.Close()
			networkAttempts++
			lastNetworkErr = true
			lastErr = fmt.Errorf("channel %s: stream died before first event: %v", cand.Channel.Name, firstErr)
			r.recordChannelFail(&cand.Channel, 0)
			excludedCh[cand.Channel.ID] = true
			if networkAttempts >= maxNetworkAttempts {
				return nil, lastErr
			}
			continue
		}
		// 稳定交付：恢复渠道并绑定
		r.recoverChannel(&cand.Channel)
		r.pen.ClearModelDenied(cand.Channel.ID, displayModel)
		r.pen.ClearModelDeniedKey(key.ID, displayModel)
		return &StreamResult{StatusCode: http.StatusOK, Stream: rc, Buffered: firstBuf,
			Channel: cand.Channel, Route: cand.Route, KeyID: key.ID, KeyOwnerID: key.UserID, Retries: networkAttempts}, nil
	}

	if protoProvided {
		// 确定性协议错误：把上游 400 原样透传，客户端看到明确原因即停止无谓重试。
		return &StreamResult{StatusCode: http.StatusBadRequest, ContentType: protoCtype, Passthrough: protoBody,
			Channel: protoChannel, Route: protoRoute, KeyID: protoKeyID, KeyOwnerID: protoKeyOwnerID, Retries: networkAttempts}, nil
	}
	if networkAttempts > 0 {
		if lastNetworkErr {
			// 全部候选都死于纯网络故障（无任何 HTTP 响应）：临时性，客户端应退避重试。
			return nil, ErrNetworkExhausted
		}
		return nil, lastErr
	}
	if len(cands) == 0 {
		return nil, ErrNoCandidate
	}
	// 有候选但全被拒权/冷却排除：临时性，客户端应退避重试。
	return nil, ErrChannelBusy
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
