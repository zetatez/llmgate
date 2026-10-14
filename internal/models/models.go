package models

import "time"

// User 为平台用户账号：可登录（username/password_hash），通过 user_tokens 消费网关。
// token_hash/token_enc 为历史遗留字段（旧版单令牌模型），不再用于鉴权，仅保留列约束。
type User struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Remark       string  `json:"remark"`
	Username     string  `json:"username"` // 登录名（空 = 未分配，不能登录）
	PasswordHash string  `json:"-"`        // bcrypt 哈希，不对外
	TokenHash    string  `json:"-"`        // 遗留字段，不再用于鉴权
	TokenEnc     string  `json:"-"`        // 遗留字段
	QuotaLimit   float64 `json:"quota_limit"`
	QuotaUsed    float64 `json:"quota_used"`
	Status       int     `json:"status"` // 1 启用 / 0 禁用
	LastLoginAt  int64   `json:"last_login_at"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

// UserToken 为一个用户可创建的网关消费令牌（sk-xxx）。一个用户可拥有多个。
type UserToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	TokenHash  string `json:"-"` // 鉴权用哈希
	TokenEnc   string `json:"-"` // AES 加密（支持回显/复制）
	Enabled    int    `json:"enabled"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

// Channel 表示一个上游套餐（渠道）。
type Channel struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	Adapter       string `json:"adapter"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
	TimeoutMS     int    `json:"timeout_ms"`
	Enabled       int    `json:"enabled"`
	HealthState   string `json:"health_state"`   // healthy / cooldown
	CooldownUntil int64  `json:"cooldown_until"` // 冷却截止（unix 秒），0=无
	ExtraHeaders  string `json:"extra_headers"`  // 渠道附加请求头（JSON 字符串），如 {"x-opencode-session":"..."}
	Note          string `json:"note"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// ChannelKey 是渠道下的一个 API Key。
type ChannelKey struct {
	ID         int64  `json:"id"`
	ChannelID  int64  `json:"channel_id"`
	UserID     int64  `json:"user_id"` // 归属用户；0 = 管理员/全局 Key
	Name       string `json:"name"`    // Key 备注名，便于区分管理
	Source     string `json:"source"`  // 来源（如官网/中转/朋友）
	Remark     string `json:"remark"`  // 备注
	APIKeyEnc  string `json:"-"`       // AES-GCM 加密后的 hex
	Enabled    int    `json:"enabled"`
	Weight     int    `json:"weight"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

// ModelRoute 定义对外模型名到上游渠道的映射。
type ModelRoute struct {
	ID            int64  `json:"id"`
	DisplayName   string `json:"display_name"`
	ChannelID     int64  `json:"channel_id"`
	UpstreamModel string `json:"upstream_model"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
	Enabled       int    `json:"enabled"`
	// 路由级自定义单价（USD / 百万 tokens）；nil = 回退全局 model_pricing（按对外模型名计费）
	PriceInput  *float64 `json:"price_input"`
	PriceOutput *float64 `json:"price_output"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// RequestLog 记录一次网关转发的用量与结果。
type RequestLog struct {
	ID               int64   `json:"id"`
	UserID           int64   `json:"user_id"`
	DisplayModel     string  `json:"display_model"`
	ChannelID        int64   `json:"channel_id"`
	KeyID            int64   `json:"key_id"`
	ChannelName      string  `json:"channel_name"` // 查询时联表填充，非存储字段
	UpstreamModel    string  `json:"upstream_model"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	LatencyMS        int64   `json:"latency_ms"`
	Status           string  `json:"status"` // success / error
	ErrorCode        string  `json:"error_code"`
	Stream           int     `json:"stream"`
	CreatedAt        int64   `json:"created_at"`
}

// Setting 为全局配置 KV。
type Setting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Now 返回当前 unix 秒（写入时间字段统一入口）。
func Now() int64 { return time.Now().Unix() }
