import { adminApi, userApi } from './client'
import type {
  AdminUserDetail, Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, ModelRoute,
  ModelUsage, PortalUser, RequestLog, SettingItem, SettingValue, UsageModelPoint, UsagePoint, UsageSummary, User,
  UserChannel, UserKey, UserModelUsage, UserOverview, UserToken,
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
  create: (d: { name?: string; remark?: string; username: string; password: string; quota_limit: number }) =>
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
  overview: () => userApi.get<DataEnvelope<UserOverview>>('/overview').then(unwrap),
  channels: () => userApi.get<DataEnvelope<UserChannel[]>>('/channels').then(unwrap),
  keys: () => userApi.get<DataEnvelope<UserKey[]>>('/keys').then(unwrap),
  addKeys: (channelId: number, keys: Array<{ name?: string; source?: string; remark?: string; key: string }>) =>
    userApi.post<DataEnvelope<{ added: number }>>('/keys', { channel_id: channelId, keys }).then(unwrap),
  updateKey: (keyId: number, d: { name?: string; source?: string; remark?: string; key?: string; enabled?: number }) =>
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
  logs: (params: { limit?: number; offset?: number } = {}) =>
    userApi.get<DataEnvelope<RequestLog[]>>('/logs', { params }).then(unwrap),
  logsCount: () => userApi.get<DataEnvelope<number>>('/logs/count').then(unwrap),
}

export const logsApi = {
  list: (params: Record<string, string | number>) =>
    adminApi.get<DataEnvelope<RequestLog[]>>('/logs', { params }).then(unwrap),
  count: (params: Record<string, string | number>) =>
    adminApi.get<DataEnvelope<number>>('/logs/count', { params }).then(unwrap),
  dashboard: () => adminApi.get<DataEnvelope<DashboardPayload>>('/logs/usage/dashboard').then(unwrap),
  usageByModel: (date?: string) =>
    adminApi.get<DataEnvelope<{ date: string; models: ModelUsage[] }>>('/logs/usage/by-model', { params: { date: date ?? '' } }).then(unwrap),
  usageByUserModel: (days: number) =>
    adminApi.get<DataEnvelope<UserModelUsage[]>>('/logs/usage/by-user-model', { params: { days } }).then(unwrap),
}

export const settingsApi = {
  get: () => adminApi.get<DataEnvelope<SettingItem[]>>('/settings').then(unwrap),
  update: (items: SettingValue[]) => adminApi.put<DataEnvelope<string>>('/settings', { data: items }).then(unwrap),
}

export interface SyncResult { at: number; created: number; skipped: number; removed: number; errored: number; cost_ms: number }

export const syncApi = {
  now: () => adminApi.post<DataEnvelope<SyncResult>>('/model-sync').then(unwrap),
}

export type { AdminUserDetail, Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, ModelRoute, ModelUsage, PortalUser, RequestLog, SettingItem, SettingValue, UsageModelPoint, UsagePoint, UsageSummary, User, UserChannel, UserKey, UserModelUsage, UserOverview, UserToken }