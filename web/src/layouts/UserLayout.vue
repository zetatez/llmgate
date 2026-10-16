<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Fold, Expand, SwitchButton } from '@element-plus/icons-vue'
import { logoutUser } from '../api/client'
import { portalApi } from '../api'
import type { PortalUser } from '../api'

const route = useRoute()
const router = useRouter()
const me = ref<PortalUser | null>(null)
const collapsed = ref(false)

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
          <span class="who">{{ displayName }}</span>
          <el-button link type="primary" @click="openPassword">Password</el-button>
          <el-button link type="warning" @click="logoutAll">Sign out all</el-button>
          <el-button link type="danger" @click="logout"><el-icon><SwitchButton /></el-icon>&nbsp;Logout</el-button>
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