<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { computed } from 'vue'
import { logoutUser } from '../api/client'
import { portalApi } from '../api'
import type { PortalUser } from '../api'

const route = useRoute()
const router = useRouter()
const me = ref<PortalUser | null>(null)

const menus = [
  { path: '/user/overview', label: 'Overview', icon: 'Odometer' },
  { path: '/user/keys', label: 'My API Keys', icon: 'Key' },
  { path: '/user/tokens', label: 'My Tokens', icon: 'Ticket' },
]

const active = computed(() => route.path)
const displayName = computed(() => me.value?.name || me.value?.username || '')

async function logout() {
  await logoutUser()
  router.push('/login')
}

onMounted(async () => {
  try { me.value = await portalApi.me() } catch { /* guard 会处理 401 */ }
})
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
        <div class="right">
          <span class="user">{{ displayName }}</span>
          <el-button link type="danger" @click="logout">Logout</el-button>
        </div>
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
.right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.user {
  color: #606266;
  font-size: 13px;
}
.main {
  background: #f0f2f5;
}
</style>
