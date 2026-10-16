<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { loginAdmin, loginUser } from '../api/client'

const router = useRouter()
const route = useRoute()

const tab = ref<'user' | 'admin'>('user')

const token = ref('')
const username = ref('')
const password = ref('')
const loading = ref(false)

function redirectTo(fallback: string) {
  const r = route.query.redirect
  // 仅接受站内路径，防止 ?redirect=//evil.com 之类被用作开放重定向
  const target = typeof r === 'string' && r.startsWith('/') && !r.startsWith('//') ? r : fallback
  router.push(target)
}

async function onAdminSubmit() {
  if (!token.value.trim()) {
    ElMessage.warning('Please enter the admin token')
    return
  }
  loading.value = true
  try {
    const ok = await loginAdmin(token.value.trim())
    if (!ok) {
      ElMessage.error('Invalid token')
      return
    }
    ElMessage.success('Logged in')
    redirectTo('/dashboard')
  } finally {
    loading.value = false
  }
}

async function onUserSubmit() {
  if (!username.value.trim() || !password.value) {
    ElMessage.warning('Please enter username and password')
    return
  }
  loading.value = true
  try {
    const ok = await loginUser(username.value.trim(), password.value)
    if (!ok) {
      ElMessage.error('Invalid username or password')
      return
    }
    ElMessage.success('Logged in')
    redirectTo('/user/overview')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <div class="blob blob-1"></div>
    <div class="blob blob-2"></div>
    <el-card class="login-card">
      <div class="brand">
        <span class="logo-mark">LG</span>
        <div class="brand-text">
          <h2 class="title">LLM Gate</h2>
          <p class="subtitle">Sign in to continue</p>
        </div>
      </div>

      <el-tabs v-model="tab" stretch>
        <el-tab-pane label="User" name="user">
          <el-form @submit.prevent="onUserSubmit">
            <el-form-item>
              <el-input v-model="username" placeholder="Username" size="large" @keyup.enter="onUserSubmit" />
            </el-form-item>
            <el-form-item>
              <el-input v-model="password" type="password" placeholder="Password" size="large" show-password @keyup.enter="onUserSubmit" />
            </el-form-item>
            <el-button type="primary" class="w-full" size="large" :loading="loading" @click="onUserSubmit">
              Sign In
            </el-button>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="Admin" name="admin">
          <el-form @submit.prevent="onAdminSubmit">
            <el-form-item>
              <el-input v-model="token" type="password" placeholder="Admin token" size="large" show-password @keyup.enter="onAdminSubmit" />
            </el-form-item>
            <el-button type="primary" class="w-full" size="large" :loading="loading" @click="onAdminSubmit">
              Sign In
            </el-button>
          </el-form>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<style scoped>
.login-wrap {
  position: relative;
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #eef0ff 0%, #f5f6fa 45%, #f6effd 100%);
  overflow: hidden;
}
.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(70px);
  opacity: 0.5;
  pointer-events: none;
}
.blob-1 {
  width: 420px;
  height: 420px;
  background: #5b6bff;
  top: -120px;
  right: -80px;
}
.blob-2 {
  width: 360px;
  height: 360px;
  background: #a78bfa;
  bottom: -120px;
  left: -60px;
}
.login-card {
  width: 400px;
  border-radius: 16px;
  box-shadow: 0 20px 60px rgba(16, 24, 40, 0.12);
  position: relative;
  z-index: 1;
}
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  justify-content: center;
  margin-bottom: 20px;
}
.logo-mark {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: linear-gradient(135deg, #5b6bff, #8b5cf6);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 18px;
  color: #fff;
  flex-shrink: 0;
}
.brand-text {
  text-align: left;
}
.title {
  margin: 0;
  font-size: 20px;
}
.subtitle {
  margin: 2px 0 0;
  color: #909399;
  font-size: 13px;
}
.w-full {
  width: 100%;
}
</style>
