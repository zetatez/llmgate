import { createRouter, createWebHistory } from 'vue-router'

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

router.beforeEach((to) => {
  const token = localStorage.getItem('llmgate_admin_token')
  if (to.name !== 'login' && !token) return { name: 'login' }
  if (to.name === 'login' && token) return { name: 'dashboard' }
})

export default router