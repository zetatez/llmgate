import { adminApi, userApi } from './client'
import type {
  AdminUserDetail, Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, MemberStat, ModelPrice, ModelRoute,
  ModelUsage, PortalUser, RequestLog, SettingItem, SettingValue, TeamUsage, UsageModelPoint, UsagePoint, UsageSummary, User,
  UserChannel, UserKey, UserModelUsage, UserOverview, UserRoute, UserToken,
} from './types'

interface DataEnvelope<T> { data: T }

const unwrap = <T>(res: { data: DataEnvelope<T> }): T => res.data.data

export const channelsApi = {
  list: () => adminApi.get<DataEnvelope<Channel[]>>('/channels').then(unwrap),
  create: (d: Partial<Channel> & { keys?: string[] }) => adminApi.post<DataEnvelope<{ id: number }>>('/channels', d).then(unwrap),
  update: (id: number, d: Partial<Channel>) => adminApi.put<DataEnvelope<Channel>>(`/channels/${id}`, d).then(unwrap),
  remove: (id: number) => adminApi.delete(`/channels/${id}`),
  test: (id: number) => adminApi.post<DataEnvelope<{ ok: boolean; error?: string; latency_ms?: number; status?: number }>>(`/channels/${id}/test`).then(unwrap),

  keys: (id: number) => adminApi.get<DataEnvelope<ChannelKeyItem[]>>(`/channels/${id}/keys`).then(unwrap),
  addKeys: (id: number, keys: Array<{ name?: string; source?: string; remark?: string; key: string }>) =>
    adminApi.post<DataEnvelope<{ added: number }>>(`/channels/${id}/keys`, { keys }).then(unwrap),
  updateKey: (keyId: number, d: { name?: string; source?: string; remark?: string; key?: string; enabled?: number; weight?: number; user_id?: number }) =>
    adminApi.put<DataEnvelope<ChannelKeyItem>>(`/channels/keys/${keyId}`, d).then(unwrap),
  removeKey: (keyId: number) => adminApi.delete(`/channels/keys/${keyId}`),
}

export const modelRoutesApi = {
  list: () => adminApi.get<DataEnvelope<ModelRoute[]>>('/model-routes').then(unwrap),
  create: (d: Partial<ModelRoute>) => adminApi.post<DataEnvelope<{ id: number }>>('/model-routes', d).then(unwrap),
  update: (id: number, d: Partial<ModelRoute>) => adminApi.put<DataEnvelope<ModelRoute>>(`/model-routes/${id}`, d).then(unwrap),
  remove: (id: number) => adminApi.delete(`/model-routes/${id}`),
}

export const usersApi = {
  list: () => adminApi.get<DataEnvelope<User[]>>('/users').then(unwrap),
  create: (d: { name?: string; remark?: string; username: string; password: string; quota_limit: number; debt_floor_calls?: number; debt_floor_credit?: number }) =>
    adminApi.post<DataEnvelope<User>>('/users', d).then(unwrap),
  update: (id: number, d: Partial<User> & { password?: string }) => adminApi.put<DataEnvelope<User>>(`/users/${id}`, d).then(unwrap),
  remove: (id: number) => adminApi.delete(`/users/${id}`),
  overview: (id: number) => adminApi.get<DataEnvelope<AdminUserDetail>>(`/users/${id}/overview`).then(unwrap),
  token: (id: number) => adminApi.get<DataEnvelope<UserToken>>(`/users/${id}/token`).then(unwrap),
  upsertToken: (id: number, d: { name?: string; expires_in_days?: number }) =>
    adminApi.put<DataEnvelope<UserToken>>(`/users/${id}/token`, d).then(unwrap),
  rotateToken: (id: number) => adminApi.post<DataEnvelope<UserToken>>(`/users/${id}/token/rotate`, {}).then(unwrap),
  removeToken: (id: number) => adminApi.delete(`/users/${id}/token`),
}

export const portalApi = {
  me: () => userApi.get<DataEnvelope<PortalUser>>('/me').then(unwrap),
  profile: () => userApi.get<DataEnvelope<User>>('/profile').then(unwrap),
  overview: () => userApi.get<DataEnvelope<UserOverview>>('/overview').then(unwrap),
  channels: () => userApi.get<DataEnvelope<UserChannel[]>>('/channels').then(unwrap),
  createChannel: (d: { name: string; base_url: string; adapter?: string; priority?: number; weight?: number; timeout_ms?: number; note?: string; enabled?: number; extra_headers?: Record<string, string>; keys?: Array<{ name?: string; source?: string; remark?: string; key: string }> }) =>
    userApi.post<DataEnvelope<{ id: number }>>('/channels', d).then(unwrap),
  updateChannel: (id: number, d: Partial<UserChannel> & { timeout_ms?: number; extra_headers?: Record<string, string> }) =>
    userApi.put<DataEnvelope<{ id: number }>>(`/channels/${id}`, d).then(unwrap),
  removeChannel: (id: number) => userApi.delete(`/channels/${id}`),
  testChannel: (id: number) =>
    userApi.post<DataEnvelope<{ ok: boolean; status?: number; error?: string; latency_ms?: number }>>(`/channels/${id}/test`, {}).then(unwrap),
  routes: () => userApi.get<DataEnvelope<UserRoute[]>>('/routes').then(unwrap),
  createRoute: (d: { display_name: string; channel_id: number; upstream_model: string; priority?: number; weight?: number; enabled?: number; price_input?: number | null; price_output?: number | null }) =>
    userApi.post<DataEnvelope<{ id: number }>>('/routes', d).then(unwrap),
  updateRoute: (id: number, d: { display_name?: string; upstream_model?: string; priority?: number; weight?: number; enabled?: number; price_input?: number | null; price_output?: number | null }) =>
    userApi.put<DataEnvelope<string>>(`/routes/${id}`, d).then(unwrap),
  removeRoute: (id: number) => userApi.delete(`/routes/${id}`),
  teamUsage: (days: number) => userApi.get<DataEnvelope<TeamUsage[]>>('/usage/by-user', { params: { days } }).then(unwrap),
  teamUsageByModel: (days: number) => userApi.get<DataEnvelope<UserModelUsage[]>>('/usage/by-user-model', { params: { days } }).then(unwrap),
  keys: () => userApi.get<DataEnvelope<UserKey[]>>('/keys').then(unwrap),
  addKeys: (channelId: number, keys: Array<{ name?: string; source?: string; remark?: string; key: string }>) =>
    userApi.post<DataEnvelope<{ added: number }>>('/keys', { channel_id: channelId, keys }).then(unwrap),
  updateKey: (keyId: number, d: { name?: string; source?: string; remark?: string; key?: string; enabled?: number; weight?: number }) =>
    userApi.put<DataEnvelope<string>>(`/keys/${keyId}`, d).then(unwrap),
  removeKey: (keyId: number) => userApi.delete(`/keys/${keyId}`),
  testKey: (keyId: number) =>
    userApi.post<DataEnvelope<{ ok: boolean; status?: number; error?: string; latency_ms?: number }>>(`/keys/${keyId}/test`).then(unwrap),
  changePassword: (currentPassword: string, newPassword: string) =>
    userApi.put<DataEnvelope<string>>('/password', { current_password: currentPassword, new_password: newPassword }).then(unwrap),
  logoutAll: () => userApi.post<DataEnvelope<string>>('/logout-all', {}).then(unwrap),
  token: () => userApi.get<DataEnvelope<UserToken>>('/token').then(unwrap),
  upsertToken: (d: { name?: string; expires_in_days?: number }) =>
    userApi.put<DataEnvelope<UserToken>>('/token', d).then(unwrap),
  rotateToken: () => userApi.post<DataEnvelope<UserToken>>('/token/rotate', {}).then(unwrap),
  removeToken: () => userApi.delete('/token'),
  logs: (params: { limit?: number; offset?: number; display_model?: string; status?: string } = {}) =>
    userApi.get<DataEnvelope<RequestLog[]>>('/logs', { params }).then(unwrap),
  logsCount: (params: { display_model?: string; status?: string } = {}) =>
    userApi.get<DataEnvelope<number>>('/logs/count', { params }).then(unwrap),
  pricing: () => userApi.get<DataEnvelope<ModelPrice[]>>('/pricing').then(unwrap),
  dashboard: (days = 7) => userApi.get<DataEnvelope<DashboardPayload>>('/dashboard', { params: { days } }).then(unwrap),
  members: () => userApi.get<DataEnvelope<MemberStat[]>>('/members').then(unwrap),
  gatewayEndpoints: () => userApi.get<DataEnvelope<string[]>>('/gateway-endpoints').then(unwrap),
  syncModels: () => userApi.post<DataEnvelope<SyncResult>>('/model-sync', {}).then(unwrap),
}

export const logsApi = {
  list: (params: Record<string, string | number>) =>
    adminApi.get<DataEnvelope<RequestLog[]>>('/logs', { params }).then(unwrap),
  count: (params: Record<string, string | number>) =>
    adminApi.get<DataEnvelope<number>>('/logs/count', { params }).then(unwrap),
  dashboard: (days = 7) =>
    adminApi.get<DataEnvelope<DashboardPayload>>('/logs/usage/dashboard', { params: { days } }).then(unwrap),
  usageByModel: (date?: string) =>
    adminApi.get<DataEnvelope<{ date: string; models: ModelUsage[] }>>('/logs/usage/by-model', { params: { date: date ?? '' } }).then(unwrap),
  usageByUserModel: (days: number) =>
    adminApi.get<DataEnvelope<UserModelUsage[]>>('/logs/usage/by-user-model', { params: { days } }).then(unwrap),
}

export const pricingApi = {
  list: () => adminApi.get<DataEnvelope<ModelPrice[]>>('/pricing').then(unwrap),
  save: (rows: Array<Pick<ModelPrice, 'model' | 'input' | 'output' | 'call'>>) =>
    adminApi.put<DataEnvelope<ModelPrice[]>>('/pricing', { data: rows }).then(unwrap),
  remove: (models: string[]) =>
    adminApi.post<DataEnvelope<ModelPrice[]>>('/pricing/delete', { models }).then(unwrap),
}

export const membersApi = {
  list: () => adminApi.get<DataEnvelope<MemberStat[]>>('/members').then(unwrap),
}

export const gatewayApi = {
  endpoints: () => adminApi.get<DataEnvelope<string[]>>('/gateway-endpoints').then(unwrap),
}

export const settingsApi = {
  get: () => adminApi.get<DataEnvelope<SettingItem[]>>('/settings').then(unwrap),
  update: (items: SettingValue[]) => adminApi.put<DataEnvelope<string>>('/settings', { data: items }).then(unwrap),
}

export interface SyncResult { at: number; created: number; skipped: number; removed: number; errored: number; cost_ms: number }

export const syncApi = {
  now: () => adminApi.post<DataEnvelope<SyncResult>>('/model-sync').then(unwrap),
}

export type { AdminUserDetail, Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, MemberStat, ModelPrice, ModelRoute, ModelUsage, PortalUser, RequestLog, SettingItem, SettingValue, TeamUsage, UsageModelPoint, UsagePoint, UsageSummary, User, UserChannel, UserKey, UserModelUsage, UserOverview, UserRoute, UserToken }