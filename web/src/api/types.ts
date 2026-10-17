// 与后端 /api/admin/* 对应的数据模型

export interface Channel {
  id: number
  user_id: number
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
  /** 仅创建用户时返回一次的网关 API_KEY 明文 */
  token?: string
  token_tail: string
  token_expires_at: number
  token_last_used_at: number
  calls_spent: number
  calls_earned: number
  calls_balance: number
  in_spent: number
  out_spent: number
  in_served: number
  out_served: number
  credit_spent: number
  credit_earned: number
  credit_balance: number
  debt_floor_calls: number
  debt_floor_credit: number
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
  // 共享池账本
  calls_spent: number
  calls_earned: number
  calls_balance: number
  in_spent: number
  out_spent: number
  in_served: number
  out_served: number
  credit_spent: number
  credit_earned: number
  credit_balance: number
  debt_floor_calls: number
  debt_floor_credit: number
}

/** 用户端：渠道（套餐，所有人可见；本人可改） */
export interface UserChannel {
  id: number
  name: string
  adapter: string
  base_url: string
  priority: number
  weight: number
  enabled: number
  health_state: string
  note: string
  user_id: number
  owner_name: string
  is_mine: boolean
  my_keys: number
}

/** 用户端：模型路由（所有人可见；本人渠道的可改） */
export interface UserRoute {
  id: number
  display_name: string
  channel_id: number
  channel_name: string
  channel_user_id: number
  upstream_model: string
  priority: number
  weight: number
  enabled: number
  effective_enabled: number
  is_mine: boolean
}

/** 共享统计：每个用户的用量（消费 + 贡献），所有人可见 */
export interface TeamUsage {
  user_id: number
  user_name: string
  requests: number
  success: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cost: number
  earned_calls: number
  earned_in_tokens: number
  earned_out_tokens: number
  earned_cost: number
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
  user_name?: string
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
/** 全局模型单价（USD / 1M tokens；call = USD / 次） */
export interface ModelPrice {
  model: string
  input: number
  output: number
  call: number
  updated_at: number
}
