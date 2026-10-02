import { createRouter, createWebHistory } from 'vue-router'
import { verifySession } from '../api/client'

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
      children: [
        { path: 'dashboard', name: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: 'Dashboard' } },
        { path: 'channels', name: 'channels', component: () => import('../views/ChannelsView.vue'), meta: { title: 'Channels' } },
        { path: 'model-routes', name: 'model-routes', component: () => import('../views/ModelRoutesView.vue'), meta: { title: 'Model Routes' } },
        { path: 'users', name: 'users', component: () => import('../views/UsersView.vue'), meta: { title: 'Users' } },
        { path: 'logs', name: 'logs', component: () => import('../views/LogsView.vue'), meta: { title: 'Logs' } },
        { path: 'settings', name: 'settings', component: () => import('../views/SettingsView.vue'), meta: { title: 'Settings' } },
      ],
    },
  ],
})

// 会话凭据为 httpOnly Cookie，前端无法同步读取，改用 /ping 异步校验。
router.beforeEach(async (to) => {
  const ok = await verifySession()
  if (to.name !== 'login' && !ok) return { name: 'login' }
  if (to.name === 'login' && ok) return { name: 'dashboard' }
})

export default router