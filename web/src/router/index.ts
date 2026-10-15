import { createRouter, createWebHistory } from 'vue-router'
import { verifySession, verifyUserSession } from '../api/client'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
    {
      path: '/',
      component: () => import('../layouts/AdminLayout.vue'),
      redirect: '/dashboard',
      meta: { area: 'admin' },
      children: [
        { path: 'dashboard', name: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: 'Dashboard', area: 'admin' } },
        { path: 'channels', name: 'channels', component: () => import('../views/ChannelsView.vue'), meta: { title: 'Channels', area: 'admin' } },
        { path: 'model-routes', name: 'model-routes', component: () => import('../views/ModelRoutesView.vue'), meta: { title: 'Model Routes', area: 'admin' } },
        { path: 'users', name: 'users', component: () => import('../views/UsersView.vue'), meta: { title: 'Users', area: 'admin' } },
        { path: 'logs', name: 'logs', component: () => import('../views/LogsView.vue'), meta: { title: 'Logs', area: 'admin' } },
        { path: 'settings', name: 'settings', component: () => import('../views/SettingsView.vue'), meta: { title: 'Settings', area: 'admin' } },
      ],
    },
    {
      path: '/user',
      component: () => import('../layouts/UserLayout.vue'),
      redirect: '/user/overview',
      meta: { area: 'user' },
      children: [
        { path: 'overview', name: 'user-overview', component: () => import('../views/user/OverviewView.vue'), meta: { title: 'Overview', area: 'user' } },
        { path: 'keys', name: 'user-keys', component: () => import('../views/user/KeysView.vue'), meta: { title: 'My API Keys', area: 'user' } },
        { path: 'tokens', name: 'user-tokens', component: () => import('../views/user/TokensView.vue'), meta: { title: 'My Tokens', area: 'user' } },
        { path: 'logs', name: 'user-logs', component: () => import('../views/user/LogsView.vue'), meta: { title: 'My Requests', area: 'user' } },
      ],
    },
  ],
})

// 会话为 httpOnly Cookie，前端无法同步读取，故按区域异步校验：
//   - admin 区域 → /api/admin/ping
//   - user  区域 → /api/user/me
router.beforeEach(async (to) => {
  const isAdminArea = to.matched.some((r) => r.meta.area === 'admin')
  const isUserArea = to.matched.some((r) => r.meta.area === 'user')

  if (to.name === 'login') {
    if (await verifySession()) return { name: 'dashboard' }
    if (await verifyUserSession()) return { name: 'user-overview' }
    return true
  }
  if (isAdminArea) {
    if (await verifySession()) return true
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (isUserArea) {
    if (await verifyUserSession()) return true
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
