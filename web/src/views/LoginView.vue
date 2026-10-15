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
    <el-card class="login-card">
      <h2 class="title">LLM Gate</h2>
      <p class="subtitle">Sign in to continue</p>

      <el-tabs v-model="tab" stretch>
        <el-tab-pane label="User" name="user">
          <el-form @submit.prevent="onUserSubmit">
            <el-form-item>
              <el-input v-model="username" placeholder="Username" @keyup.enter="onUserSubmit" />
            </el-form-item>
            <el-form-item>
              <el-input v-model="password" type="password" placeholder="Password" show-password @keyup.enter="onUserSubmit" />
            </el-form-item>
            <el-button type="primary" class="w-full" :loading="loading" @click="onUserSubmit">
              Sign In
            </el-button>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="Admin" name="admin">
          <el-form @submit.prevent="onAdminSubmit">
            <el-form-item>
              <el-input v-model="token" type="password" placeholder="Admin token" show-password @keyup.enter="onAdminSubmit" />
            </el-form-item>
            <el-button type="primary" class="w-full" :loading="loading" @click="onAdminSubmit">
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
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f0f2f5;
}
.login-card {
  width: 380px;
}
.title {
  text-align: center;
  margin: 0 0 4px;
}
.subtitle {
  text-align: center;
  color: #999;
  font-size: 13px;
  margin: 0 0 16px;
}
.w-full {
  width: 100%;
}
</style>
