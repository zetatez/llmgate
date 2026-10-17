package models

import "time"

// User 为平台用户账号：可登录（username/password_hash），并持有一个网关消费令牌。
// 令牌直接存于本表（token_hash 鉴权 / token_enc 加密回显），一人一个。
type User struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Remark         string  `json:"remark"`
	Username       string  `json:"username"` // 登录名（空 = 未分配，不能登录）
	PasswordHash   string  `json:"-"`        // bcrypt 哈希，不对外
	TokenHash      string  `json:"-"`        // 消费令牌哈希（鉴权用）
	TokenEnc       string  `json:"-"`        // AES 加密的令牌（支持回显）
	TokenExpiresAt int64   `json:"-"`        // 令牌过期时间（unix 秒），0 = 永不过期
	TokenLastUsed  int64   `json:"-"`        // 令牌最近使用时间
	QuotaLimit     float64 `json:"quota_limit"`
	QuotaUsed      float64 `json:"quota_used"`
	Status         int     `json:"status"` // 1 启用 / 0 禁用
	// 共享池双向账本
	CallsSpent      int64   `json:"calls_spent"`
	CallsEarned     int64   `json:"calls_earned"`
	InSpent         int64   `json:"in_spent"`
	OutSpent        int64   `json:"out_spent"`
	InServed        int64   `json:"in_served"`
	OutServed       int64   `json:"out_served"`
	CreditSpent     float64 `json:"credit_spent"`
	CreditEarned    float64 `json:"credit_earned"`
	BaseCalls       int64   `json:"base_calls"`
	BaseIn          int64   `json:"base_in"`
	BaseOut         int64   `json:"base_out"`
	BaseCredit      float64 `json:"base_credit"`
	DebtFloorCalls  int64   `json:"debt_floor_calls"`
	DebtFloorCredit float64 `json:"debt_floor_credit"`
	LastLoginAt     int64   `json:"last_login_at"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
}

// Channel 表示一个上游套餐（渠道）。
type Channel struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"` // 归属用户；0 = 管理员/全局
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
	UserName         string  `json:"user_name"` // 查询时联表填充，非存储字段
	DisplayModel     string  `json:"display_model"`
	ChannelID        int64   `json:"channel_id"`
	KeyID            int64   `json:"key_id"`
	KeyOwnerID       int64   `json:"key_owner_id"` // 贡献者（Key 归属用户），0=平台
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

// ModelPrice 全局模型单价（对外模型名维度）。
type ModelPrice struct {
	Model     string  `json:"model"`
	Input     float64 `json:"input"`  // USD / 1M input tokens
	Output    float64 `json:"output"` // USD / 1M output tokens
	Call      float64 `json:"call"`   // USD / request
	UpdatedAt int64   `json:"updated_at"`
}

// Now 返回当前 unix 秒（写入时间字段统一入口）。
func Now() int64 { return time.Now().Unix() }
