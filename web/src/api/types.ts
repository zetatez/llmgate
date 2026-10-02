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
  token?: string
  token_tail?: string
  quota_limit: number
  quota_used: number
  status: number
  created_at: number
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

export interface DashboardPayload {
  week: UsagePoint[]
  week_models: UsageModelPoint[]
  today: UsageSummary
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
  key: string
  value: string
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