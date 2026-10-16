// 与后端 /api/admin/* 对应的数据模型

export interface Channel {
  id: number
  name: string
  base_url: string
  adapter: string
  priority: number
  weight: number
  timeout_ms: number
  enabled: number
  health_state: string
  extra_headers: string
  note: string
  created_at: number
  updated_at: number
}

export interface ChannelKeyItem {
  id: number
  channel_id: number
  user_id: number
  owner_name: string
  name: string
  source: string
  remark: string
  masked: string
  key_tail: string
  enabled: number
  effective_enabled: number
  weight: number
  last_used_at: number
}

export interface ModelRoute {
  id: number
  display_name: string
  channel_id: number
  channel_name: string
  channel_enabled: number
  effective_enabled: number
  upstream_model: string
  priority: number
  weight: number
  enabled: number
  /** 路由级自定义单价 USD/百万tokens；null = 回退全局 model_pricing */
  price_input?: number | null
  price_output?: number | null
}

export interface User {
  id: number
  name: string
  remark: string
  username: string
  quota_limit: number
  quota_used: number
  status: number
  has_token: boolean
  token_tail: string
  token_expires_at: number
  token_last_used_at: number
  last_login_at: number
  created_at: number
}

/** 消费令牌（网关 sk-xxx）：直接存在 users 表内，一人一个。 */
export interface UserToken {
  /** 明文（可随时回显）；无令牌时为空 */
  token?: string
  token_tail: string
  has_token: boolean
  expires_at: number
  last_used_at: number
}

/** 用户端：登录后的自身信息 */
export interface PortalUser {
  id: number
  username: string
  name: string
  quota_limit: number
  quota_used: number
  status: number
  last_login_at: number
  gateway_prefix: string
}

/** 用户端：可录入 Key 的渠道（不暴露上游 base_url） */
export interface UserChannel {
  id: number
  name: string
  adapter: string
  my_keys: number
}

/** 用户端：本人录入的 API Key */
export interface UserKey {
  id: number
  channel_id: number
  channel_name: string
  name: string
  source: string
  remark: string
  masked: string
  key_tail: string
  enabled: number
  last_used_at: number
  created_at: number
}

/** 用户端：用量概览 */
export interface UserOverview {
  week: UsagePoint[]
  today: UsageSummary
  total: UsageSummary
}

/** 管理员：某用户聚合详情 */
export interface AdminUserDetail {
  user: User
  keys: UserKey[]
  token: UserToken
  today: UsageSummary
  total: UsageSummary
  by_model: ModelUsage[]
}

export interface RequestLog {
  id: number
  user_id: number
  display_model: string
  channel_id: number
  key_id: number
  channel_name?: string
  upstream_model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cost: number
  latency_ms: number
  status: string
  error_code: string
  stream: number
  created_at: number
}

export interface UsagePoint {
  date: string
  requests: number
  success: number
  errors: number
  total_tokens: number
  prompt_tokens: number
  cost: number
}

export interface UsageSummary {
  total_requests: number
  success: number
  errors: number
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cost: number
}

export interface ChannelDayStat {
  channel_id: number
  name: string
  health: string
  today_requests: number
  today_errors: number
}

export interface UsageHourPoint {
  hour: string
  requests: number
  tokens: number
  cost: number
}

export interface DashboardPayload {
  days: number
  week: UsagePoint[]
  week_models: UsageModelPoint[]
  hours: UsageHourPoint[]
  today: UsageSummary
  periods: { today: UsageSummary; week: UsageSummary; month: UsageSummary }
  channel_stats: ChannelDayStat[]
  failures: RequestLog[]
  recent_failures: number
}

export interface UsageModelPoint {
  date: string
  model: string
  requests: number
  tokens: number
}

export interface SettingItem {
  classification: string
  key: string
  val: string
  description: string
}

/** 更新设置时的载荷：仅需 key/val（分类与描述由后端下发）。 */
export interface SettingValue {
  key: string
  val: string
}

export interface ModelUsage {
  model: string
  requests: number
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cost: number
}

export interface UserModelUsage {
  user_id: number
  user_name: string
  model: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cost: number
}