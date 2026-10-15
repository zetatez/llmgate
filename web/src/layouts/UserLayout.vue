<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
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
  { path: '/user/logs', label: 'My Requests', icon: 'Document' },
]

const active = computed(() => route.path)
const displayName = computed(() => me.value?.name || me.value?.username || '')

async function logout() {
  await logoutUser()
  router.push('/login')
}

// 注销所有设备（吊销全部会话）
async function logoutAll() {
  try {
    await ElMessageBox.confirm('Sign out of all devices? Every active session will be revoked.', 'Sign out all devices', { type: 'warning' })
  } catch { return }
  try {
    await portalApi.logoutAll()
    router.push('/login')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to sign out')
  }
}

// 自助改密码
const pwDialog = reactive({ visible: false, current: '', next: '', confirm: '', busy: false })

function openPassword() {
  Object.assign(pwDialog, { visible: true, current: '', next: '', confirm: '', busy: false })
}

async function submitPassword() {
  if (!pwDialog.current) { ElMessage.warning('Enter your current password'); return }
  if (pwDialog.next.length < 6) { ElMessage.warning('New password must be at least 6 characters'); return }
  if (pwDialog.next !== pwDialog.confirm) { ElMessage.warning('Passwords do not match'); return }
  pwDialog.busy = true
  try {
    await portalApi.changePassword(pwDialog.current, pwDialog.next)
    ElMessage.success('Password updated')
    pwDialog.visible = false
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to update password')
  } finally {
    pwDialog.busy = false
  }
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
          <el-button link type="primary" @click="openPassword">Password</el-button>
          <el-button link type="warning" @click="logoutAll">Sign out all</el-button>
          <el-button link type="danger" @click="logout">Logout</el-button>
        </div>
      </el-header>
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>

    <el-dialog v-model="pwDialog.visible" title="Change Password" width="420px">
      <el-form label-width="130px">
        <el-form-item label="Current password">
          <el-input v-model="pwDialog.current" type="password" show-password />
        </el-form-item>
        <el-form-item label="New password">
          <el-input v-model="pwDialog.next" type="password" show-password placeholder="at least 6 characters" />
        </el-form-item>
        <el-form-item label="Confirm">
          <el-input v-model="pwDialog.confirm" type="password" show-password @keyup.enter="submitPassword" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwDialog.visible = false">Cancel</el-button>
        <el-button type="primary" :loading="pwDialog.busy" @click="submitPassword">Save</el-button>
      </template>
    </el-dialog>
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
