import axios from 'axios'

// 管理端 API client：会话凭据为 httpOnly Cookie（不再用 localStorage）。
// 同源请求交给浏览器自动附带 Cookie，withCredentials 保障将来跨源（反代）场景。
export const adminApi = axios.create({
  baseURL: '/api/admin',
  withCredentials: true,
})

adminApi.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

// 登录：校验管理令牌并种下 httpOnly 会话 Cookie（令牌本身不进前端存储）。
export async function loginAdmin(token: string): Promise<boolean> {
  try {
    await axios.post(
      '/api/admin/login',
      {},
      { headers: { Authorization: `Bearer ${token}` }, withCredentials: true },
    )
    return true
  } catch {
    return false
  }
}

// 登出：清除 httpOnly 会话 Cookie。
export async function logoutAdmin(): Promise<void> {
  try {
    await axios.post('/api/admin/logout', {}, { withCredentials: true })
  } catch {
    /* 忽略，登出失败也要继续跳转 */
  }
}

// 校验当前管理员会话（Cookie）是否有效（登录页/路由守卫用）。
export async function verifySession(): Promise<boolean> {
  try {
    const res = await fetch('/api/admin/ping', { credentials: 'same-origin' })
    return res.ok
  } catch {
    return false
  }
}

export type SessionRole = 'admin' | 'user' | 'anonymous'

// 一次请求获取当前会话身份（供路由守卫判定，避免两次串行校验）。
export async function getSession(): Promise<{ role: SessionRole }> {
  try {
    const res = await fetch('/api/session', { credentials: 'same-origin' })
    if (!res.ok) return { role: 'anonymous' }
    const body = await res.json()
    return { role: (body?.role as SessionRole) || 'anonymous' }
  } catch {
    return { role: 'anonymous' }
  }
}

// 用户端 API client：会话凭据同样是 httpOnly Cookie（与管理员端相互独立）。
export const userApi = axios.create({
  baseURL: '/api/user',
  withCredentials: true,
})

userApi.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

// 登录：用户名 + 密码 → 建立用户端会话。
export async function loginUser(username: string, password: string): Promise<boolean> {
  try {
    await axios.post('/api/user/login', { username, password }, { withCredentials: true })
    return true
  } catch {
    return false
  }
}

// 登出：清除用户端会话 Cookie。
export async function logoutUser(): Promise<void> {
  try {
    await axios.post('/api/user/logout', {}, { withCredentials: true })
  } catch {
    /* 忽略 */
  }
}

// 校验当前用户端会话是否有效。
export async function verifyUserSession(): Promise<boolean> {
  try {
    const res = await fetch('/api/user/me', { credentials: 'same-origin' })
    return res.ok
  } catch {
    return false
  }
}