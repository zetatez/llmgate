<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { adminTokenKey } from '../api/client'

const route = useRoute()
const router = useRouter()

const menus = [
  { path: '/dashboard', label: 'Dashboard', icon: 'Odometer' },
  { path: '/channels', label: 'Channels', icon: 'Connection' },
  { path: '/model-routes', label: 'Model Routes', icon: 'Share' },
  { path: '/users', label: 'Users', icon: 'User' },
  { path: '/logs', label: 'Logs', icon: 'Document' },
  { path: '/settings', label: 'Settings', icon: 'Setting' },
]

const active = computed(() => route.path)

function logout() {
  localStorage.removeItem(adminTokenKey)
  router.push('/login')
}
</script>

<template>
  <el-container class="layout">
    <el-aside width="200px" class="aside">
      <div class="logo">LLM Gate</div>
      <el-menu :default-active="active" router class="menu">
        <el-menu-item v-for="m in menus" :key="m.path" :index="m.path">
          <el-icon><component :is="m.icon" /></el-icon>
          <span>{{ m.label }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <span class="page-title">{{ route.meta.title || '' }}</span>
        <el-button link type="danger" @click="logout">Logout</el-button>
      </el-header>
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  height: 100vh;
}
.aside {
  background: #001529;
}
.logo {
  color: #fff;
  font-size: 20px;
  font-weight: 600;
  text-align: center;
  line-height: 60px;
}
.menu {
  border-right: none;
  background: transparent;
  --el-menu-text-color: #cfd3dc;
  --el-menu-hover-bg-color: #001529;
  --el-menu-active-color: #fff;
}
.menu :deep(.el-menu-item.is-active) {
  background: #1677ff;
}
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #eee;
  background: #fff;
}
.page-title {
  font-size: 16px;
  font-weight: 600;
}
.main {
  background: #f0f2f5;
}
</style>