import { adminApi } from './client'
import type {
  Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, ModelRoute,
  ModelUsage, RequestLog, SettingItem, UsagePoint, UsageSummary, User, UserModelUsage,
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
  updateKey: (keyId: number, d: { name?: string; source?: string; remark?: string; key?: string; enabled?: number; weight?: number }) =>
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
  create: (d: { name: string; remark?: string; quota_limit: number }) => adminApi.post<DataEnvelope<User>>('/users', d).then(unwrap),
  update: (id: number, d: Partial<User>) => adminApi.put<DataEnvelope<User>>(`/users/${id}`, d).then(unwrap),
  remove: (id: number) => adminApi.delete(`/users/${id}`),
  getToken: (id: number) => adminApi.get<DataEnvelope<{ token: string }>>(`/users/${id}/token`).then(unwrap),
  resetToken: (id: number) => adminApi.post<DataEnvelope<{ token: string }>>(`/users/${id}/token`).then(unwrap),
}

export const logsApi = {
  list: (params: Record<string, string | number>) =>
    adminApi.get<DataEnvelope<RequestLog[]>>('/logs', { params }).then(unwrap),
  dashboard: () => adminApi.get<DataEnvelope<DashboardPayload>>('/logs/usage/dashboard').then(unwrap),
  usageByModel: (date?: string) =>
    adminApi.get<DataEnvelope<{ date: string; models: ModelUsage[] }>>('/logs/usage/by-model', { params: { date: date ?? '' } }).then(unwrap),
  usageByUserModel: (days: number) =>
    adminApi.get<DataEnvelope<UserModelUsage[]>>('/logs/usage/by-user-model', { params: { days } }).then(unwrap),
}

export const settingsApi = {
  get: () => adminApi.get<DataEnvelope<SettingItem[]>>('/settings').then(unwrap),
  update: (items: SettingItem[]) => adminApi.put<DataEnvelope<string>>('/settings', { data: items }).then(unwrap),
}

export interface SyncResult { at: number; created: number; skipped: number; removed: number; errored: number; cost_ms: number }

export const syncApi = {
  now: () => adminApi.post<DataEnvelope<SyncResult>>('/model-sync').then(unwrap),
}

export type { Channel, ChannelDayStat, ChannelKeyItem, DashboardPayload, ModelRoute, ModelUsage, RequestLog, SettingItem, UsagePoint, UsageSummary, User, UserModelUsage }