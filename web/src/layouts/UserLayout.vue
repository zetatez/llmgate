<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { SwitchButton } from '@element-plus/icons-vue'
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
        <span class="who">{{ displayName }}</span>
        <el-button link type="primary" @click="openPassword">Password</el-button>
        <el-button link type="warning" @click="logoutAll">Sign out all</el-button>
        <el-button link type="danger" @click="logout"><el-icon><SwitchButton /></el-icon>&nbsp;Logout</el-button>
      </div>
    </header>
    <main class="main">
      <div class="page-title">{{ route.meta.title || '' }}</div>
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>

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
  </div>
</template>

<style scoped>
.layout {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
.topbar {
  height: 48px;
  background: #14161f;
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
  color: #fff;
  margin-right: 12px;
  flex-shrink: 0;
}
.logo-mark {
  width: 26px;
  height: 26px;
  border-radius: 8px;
  background: linear-gradient(135deg, #5b6bff, #8b5cf6);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 12px;
  color: #fff;
}
.logo-text {
  font-size: 14px;
  font-weight: 700;
  white-space: nowrap;
}
.topmenu {
  flex: 1;
  min-width: 0;
  background: transparent;
  border-bottom: none;
  --el-menu-bg-color: transparent;
  --el-menu-text-color: #a2a8b8;
  --el-menu-hover-text-color: #ffffff;
  --el-menu-active-color: #ffffff;
  --el-menu-item-height: 48px;
}
.topmenu :deep(.el-menu-item) {
  border-bottom: 2px solid transparent;
}
.topmenu :deep(.el-menu-item:hover) {
  background: #1d2030;
}
.topmenu :deep(.el-menu-item.is-active) {
  color: #fff;
  border-bottom: 2px solid #7c89ff;
  background: linear-gradient(180deg, transparent, rgba(124, 137, 255, 0.18));
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
  color: #a2a8b8;
  font-size: 12px;
}
.main {
  flex: 1;
  padding: 16px;
  background: var(--ll-bg);
}
.page-title {
  font-size: 16px;
  font-weight: 700;
  margin-bottom: 14px;
  color: #1f2430;
}
</style>