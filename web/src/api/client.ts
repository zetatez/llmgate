import axios from 'axios'

export const adminTokenKey = 'llmgate_admin_token'

// 管理端 API client：自动附带 admin token，401 时跳登录。
export const adminApi = axios.create({
  baseURL: '/api/admin',
})

adminApi.interceptors.request.use((config) => {
  const token = localStorage.getItem(adminTokenKey)
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

adminApi.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem(adminTokenKey)
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

// 校验 token 是否可用（登录页调用）。
export async function verifyAdminToken(token: string): Promise<boolean> {
  try {
    await axios.get('/api/admin/ping', { headers: { Authorization: `Bearer ${token}` } })
    return true
  } catch {
    return false
  }
}