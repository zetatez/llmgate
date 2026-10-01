<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { verifyAdminToken, adminTokenKey } from '../api/client'

const router = useRouter()
const route = useRoute()
const token = ref('')
const loading = ref(false)

async function onSubmit() {
  if (!token.value.trim()) {
    ElMessage.warning('Please enter the admin token')
    return
  }
  loading.value = true
  try {
    const ok = await verifyAdminToken(token.value.trim())
    if (!ok) {
      ElMessage.error('Invalid token')
      return
    }
    localStorage.setItem(adminTokenKey, token.value.trim())
    ElMessage.success('Logged in')
    router.push(route.query.redirect ? String(route.query.redirect) : '/dashboard')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <h2 class="title">LLM Gate Admin</h2>
      <p class="subtitle">Enter your admin token to continue</p>
      <el-form @submit.prevent="onSubmit">
        <el-form-item>
          <el-input
            v-model="token"
            type="password"
            placeholder="Admin token"
            show-password
            @keyup.enter="onSubmit"
          />
        </el-form-item>
        <el-button type="primary" class="w-full" :loading="loading" @click="onSubmit">
          Sign In
        </el-button>
      </el-form>
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
  width: 360px;
}
.title {
  text-align: center;
  margin: 0 0 4px;
}
.subtitle {
  text-align: center;
  color: #999;
  font-size: 13px;
  margin: 0 0 24px;
}
.w-full {
  width: 100%;
}
</style>