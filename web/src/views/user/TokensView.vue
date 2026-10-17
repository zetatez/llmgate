<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi } from '../../api'
import type { PortalUser, UserToken } from '../../api'

const token = ref<UserToken | null>(null)
const me = ref<PortalUser | null>(null)
const loading = ref(false)
const busy = ref(false)
const reveal = ref(false)
const days = ref(0)

const hasToken = computed(() => !!token.value?.has_token)

const gatewayBase = computed(() => {
  const prefix = me.value?.gateway_prefix || '/'
  const p = prefix === '/' ? '' : prefix.replace(/\/$/, '')
  return `${window.location.origin}${p}/v1`
})

function copyBase() {
  navigator.clipboard?.writeText(gatewayBase.value)
  ElMessage.success('Copied')
}

function copyToken() {
  if (!token.value?.token) return
  navigator.clipboard?.writeText(token.value.token)
  ElMessage.success('Copied')
}

function fmt(ts?: number) {
  return ts ? new Date(ts * 1000).toLocaleString() : 'never'
}

async function load() {
  loading.value = true
  try {
    token.value = await portalApi.token()
    if (!me.value) me.value = await portalApi.me()
  } finally {
    loading.value = false
  }
}

// 创建（或在已有令牌时仅更新过期时间，不轮换密钥）
async function save() {
  busy.value = true
  const wasToken = hasToken.value
  try {
    token.value = await portalApi.upsertToken({ expires_in_days: days.value })
    ElMessage.success(wasToken ? 'Saved' : 'API_KEY created')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to save API_KEY')
  } finally {
    busy.value = false
  }
}

async function rotate() {
  try {
    await ElMessageBox.confirm('Refresh the API_KEY? The current one stops working immediately.', 'Refresh API_KEY', { type: 'warning' })
  } catch { return }
  busy.value = true
  try {
    token.value = await portalApi.rotateToken()
    reveal.value = true
    ElMessage.success('API_KEY refreshed')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to refresh')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <el-alert type="info" :closable="false" class="mb">
      <template #title>
        <span>Gateway Base URL: <code class="base">{{ gatewayBase }}</code></span>
        <el-button link type="primary" size="small" class="ml" @click="copyBase">copy</el-button>
      </template>
    </el-alert>

    <el-card shadow="never" v-loading="loading">

      <template v-if="!hasToken">
        <el-empty description="You don't have an API_KEY yet." />
        <el-form label-width="120px" class="form">
          <el-form-item label="Expires">
            <el-select v-model="days" style="width: 220px">
              <el-option label="Never" :value="0" />
              <el-option label="7 days" :value="7" />
              <el-option label="30 days" :value="30" />
              <el-option label="90 days" :value="90" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="busy" @click="save">Create API_KEY</el-button>
          </el-form-item>
        </el-form>
      </template>

      <template v-else>
        <el-descriptions :column="1" border class="mb">
          <el-descriptions-item label="API_KEY">
            <span class="token">
              <code>{{ reveal && token?.token ? token.token : `sk-****${token?.token_tail}` }}</code>
              <el-button v-if="token?.token" link type="primary" size="small" @click="reveal = !reveal">{{ reveal ? 'Hide' : 'Reveal' }}</el-button>
              <el-button v-if="token?.token" link type="primary" size="small" @click="copyToken">copy</el-button>
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="Expires">{{ fmt(token?.expires_at) }}</el-descriptions-item>
          <el-descriptions-item label="Last used">{{ fmt(token?.last_used_at) }}</el-descriptions-item>
        </el-descriptions>

        <el-form label-width="120px" class="form">
          <el-form-item label="Set expiry">
            <el-select v-model="days" style="width: 220px">
              <el-option label="Never" :value="0" />
              <el-option label="7 days" :value="7" />
              <el-option label="30 days" :value="30" />
              <el-option label="90 days" :value="90" />
            </el-select>
            <span class="tip">sets expiry from now</span>
          </el-form-item>
          <el-form-item>
            <el-button :loading="busy" @click="save">Save</el-button>
            <el-button type="warning" :loading="busy" @click="rotate">refresh</el-button>
          </el-form-item>
        </el-form>
      </template>
    </el-card>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 10px; }
.ml { margin-left: 8px; }
.tip { font-size: 12px; color: var(--ll-muted); margin-left: 8px; }
.base { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; color: var(--el-color-primary); }
.token { display: inline-flex; align-items: center; gap: 8px; }
.token code { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.form { margin-top: 8px; max-width: 560px; }
</style>
