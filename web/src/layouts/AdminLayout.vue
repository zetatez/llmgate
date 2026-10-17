<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { logoutAdmin } from '../api/client'
import { SwitchButton } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()

const menus = [
  { path: '/dashboard', label: 'Overview', icon: 'Odometer' },
  { path: '/channels', label: 'Channels', icon: 'Connection' },
  { path: '/model-routes', label: 'Routes', icon: 'Share' },
  { path: '/users', label: 'Users', icon: 'User' },
  { path: '/usage', label: 'Usage', icon: 'DataLine' },
  { path: '/logs', label: 'Logs', icon: 'Document' },
  { path: '/settings', label: 'Settings', icon: 'Setting' },
]

const active = computed(() => route.path)

async function logout() {
  await logoutAdmin()
  router.push('/login')
}
</script>

<template>
  <div class="layout">
    <header class="topbar">
      <div class="logo">
        <span class="logo-mark">LG</span>
        <span class="logo-text">LLM Gate</span>
      </div>
      <el-menu :default-active="active" mode="horizontal" router class="topmenu" :ellipsis="false">
        <el-menu-item v-for="m in menus" :key="m.path" :index="m.path">{{ m.label }}</el-menu-item>
      </el-menu>
      <div class="right">
        <span class="who">Admin</span>
        <el-button link type="danger" @click="logout">
          <el-icon><SwitchButton /></el-icon>&nbsp;Logout
        </el-button>
      </div>
    </header>
    <main class="main">
      <div class="page">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </div>
    </main>
  </div>
</template>

<style scoped>
.layout {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
.topbar {
  height: 40px;
  background: var(--ll-card);
  border-bottom: 1px solid var(--ll-border);
  display: flex;
  align-items: center;
  padding: 0 16px;
  gap: 8px;
  position: sticky;
  top: 0;
  z-index: 20;
}
.logo {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ll-ink);
  margin-right: 12px;
  flex-shrink: 0;
}
.logo-mark {
  width: 24px;
  height: 24px;
  border-radius: 8px;
  background: linear-gradient(135deg, #8b93d6, #b5a7dd);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 12px;
  color: #fff;
}
.logo-text {
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}
.topmenu {
  flex: 1;
  min-width: 0;
  background: transparent;
  border-bottom: none;
  --el-menu-bg-color: transparent;
  --el-menu-text-color: var(--ll-muted);
  --el-menu-hover-text-color: var(--ll-ink);
  --el-menu-active-color: var(--el-color-primary);
  --el-menu-item-height: 40px;
}
.topmenu :deep(.el-menu-item) {
  border-bottom: 2px solid transparent;
}
.topmenu :deep(.el-menu-item:hover) {
  background: var(--ll-soft);
}
.topmenu :deep(.el-menu-item.is-active) {
  color: var(--el-color-primary);
  border-bottom: 2px solid #8b93d6;
  background: transparent;
}
.m-label {
  margin-left: 6px;
}
.right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
.who {
  color: var(--ll-muted);
  font-size: 12px;
}
.main {
  flex: 1;
  padding: 16px 24px;
  background: var(--ll-bg);
}
.page {
  width: 100%;
  max-width: 1400px;
  margin: 0 auto;
}
</style>