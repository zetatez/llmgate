package models

import "time"

// User 为轻量用户，每个用户持有一个独立令牌 sk-xxx。
type User struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Remark     string  `json:"remark"`
	TokenHash  string  `json:"-"` // 令牌哈希（鉴权用），不对外
	TokenEnc   string  `json:"-"` // AES 加密的令牌（支持管理员回显/复制）
	QuotaLimit float64 `json:"quota_limit"`
	QuotaUsed  float64 `json:"quota_used"`
	Status     int     `json:"status"` // 1 启用 / 0 禁用
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
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
	Note          string `json:"note"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// ChannelKey 是渠道下的一个 API Key。
type ChannelKey struct {
	ID         int64  `json:"id"`
	ChannelID  int64  `json:"channel_id"`
	Name       string `json:"name"`   // Key 备注名，便于区分管理
	Source     string `json:"source"` // 来源（如官网/中转/朋友）
	Remark     string `json:"remark"` // 备注
	APIKeyEnc  string `json:"-"`      // AES-GCM 加密后的 hex
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
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// RequestLog 记录一次网关转发的用量与结果。
type RequestLog struct {
	ID               int64   `json:"id"`
	UserID           int64   `json:"user_id"`
	DisplayModel     string  `json:"display_model"`
	ChannelID        int64   `json:"channel_id"`
	KeyID            int64   `json:"key_id"`
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
