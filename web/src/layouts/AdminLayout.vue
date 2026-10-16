<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { logoutAdmin } from '../api/client'
import { Fold, Expand, SwitchButton } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const collapsed = ref(false)

const menus = [
  { path: '/dashboard', label: 'Dashboard', icon: 'Odometer' },
  { path: '/channels', label: 'Channels', icon: 'Connection' },
  { path: '/model-routes', label: 'Model Routes', icon: 'Share' },
  { path: '/users', label: 'Users', icon: 'User' },
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
  <el-container class="layout">
    <el-aside :width="collapsed ? '64px' : '210px'" class="aside">
      <div class="logo">
        <span class="logo-mark">LG</span>
        <span v-show="!collapsed" class="logo-text">LLM Gate</span>
      </div>
      <el-menu :default-active="active" :collapse="collapsed" :collapse-transition="false" router class="menu">
        <el-menu-item v-for="m in menus" :key="m.path" :index="m.path">
          <el-icon><component :is="m.icon" /></el-icon>
          <template #title>{{ m.label }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <div class="left">
          <el-button text class="collapse-btn" @click="collapsed = !collapsed">
            <el-icon :size="18"><component :is="collapsed ? Expand : Fold" /></el-icon>
          </el-button>
          <span class="page-title">{{ route.meta.title || '' }}</span>
        </div>
        <div class="right">
          <span class="who">Admin</span>
          <el-button link type="danger" @click="logout">
            <el-icon><SwitchButton /></el-icon>&nbsp;Logout
          </el-button>
        </div>
      </el-header>
      <el-main class="main">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  height: 100vh;
}
.aside {
  transition: width 0.2s ease;
  overflow: hidden;
  background: #14161f;
  display: flex;
  flex-direction: column;
}
.logo {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #fff;
  flex-shrink: 0;
}
.logo-mark {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  background: linear-gradient(135deg, #5b6bff, #8b5cf6);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 13px;
  color: #fff;
  flex-shrink: 0;
}
.logo-text {
  font-size: 16px;
  font-weight: 700;
  white-space: nowrap;
}
.menu {
  border-right: none;
  background: transparent;
  --el-menu-text-color: #a2a8b8;
  --el-menu-hover-bg-color: #1d2030;
  --el-menu-active-color: #ffffff;
}
.menu :deep(.el-menu-item.is-active) {
  background: linear-gradient(90deg, #5b6bff, #7c66f5);
}
.menu :deep(.el-menu-item:hover) {
  color: #fff;
}
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #ececf1;
  background: #fff;
  height: 60px;
  padding: 0 16px;
}
.left {
  display: flex;
  align-items: center;
  gap: 6px;
}
.collapse-btn {
  font-size: 16px;
  color: #6b7280;
}
.page-title {
  font-size: 15px;
  font-weight: 600;
}
.right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.who {
  color: #6b7280;
  font-size: 13px;
}
.main {
  background: var(--ll-bg);
  padding: 16px;
}
</style>