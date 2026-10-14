<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi } from '../../api'
import type { PortalUser, UserToken } from '../../api'

const tokens = ref<UserToken[]>([])
const me = ref<PortalUser | null>(null)
const loading = ref(false)

const gatewayBase = computed(() => {
  const prefix = me.value?.gateway_prefix || '/'
  const p = prefix === '/' ? '' : prefix.replace(/\/$/, '')
  return `${window.location.origin}${p}/v1`
})

function copyBase() {
  navigator.clipboard?.writeText(gatewayBase.value)
  ElMessage.success('Copied')
}

async function load() {
  loading.value = true
  try {
    tokens.value = await portalApi.tokens()
    if (!me.value) me.value = await portalApi.me()
  } finally {
    loading.value = false
  }
}

const dialog = reactive({ visible: false, name: '' })
const reveal = reactive({ visible: false, token: '' })

function openAdd() {
  Object.assign(dialog, { visible: true, name: '' })
}

async function create() {
  try {
    const t = await portalApi.createToken(dialog.name)
    dialog.visible = false
    reveal.token = t.token || ''
    reveal.visible = true
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to create token')
  }
}

async function toggle(t: UserToken) {
  const next = t.enabled ? 0 : 1
  try {
    await portalApi.updateToken(t.id, { enabled: next })
    t.enabled = next
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Operation failed')
  }
}

async function remove(t: UserToken) {
  try {
    await ElMessageBox.confirm(`Delete token "${t.name || t.token_tail}"? Clients using it will lose access immediately.`, 'Delete Token', { type: 'warning' })
  } catch { return }
  try {
    await portalApi.removeToken(t.id)
    tokens.value = tokens.value.filter((x) => x.id !== t.id)
    ElMessage.success('Deleted')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to delete')
  }
}

function copyReveal() {
  navigator.clipboard?.writeText(reveal.token)
  ElMessage.success('Copied')
}

onMounted(load)
</script>

<template>
  <div>
    <el-alert type="info" :closable="false" class="mb">
      <template #title>
        <span>Gateway Base URL: <code class="base">{{ gatewayBase }}</code></span>
        <el-button link type="primary" size="small" class="ml" @click="copyBase">Copy</el-button>
      </template>
    </el-alert>

    <el-card shadow="never" v-loading="loading">
      <template #header>
        <div class="head">
          <span>My Tokens</span>
          <el-button type="primary" size="small" @click="openAdd">Create Token</el-button>
        </div>
      </template>

      <el-table :data="tokens" size="small">
        <el-table-column label="Name" min-width="140">
          <template #default="{ row }">{{ row.name || 'Unnamed' }}</template>
        </el-table-column>
        <el-table-column label="Token" width="180">
          <template #default="{ row }"><code class="key">sk-****{{ row.token_tail }}</code></template>
        </el-table-column>
        <el-table-column label="Last used" width="170">
          <template #default="{ row }">{{ row.last_used_at ? new Date(row.last_used_at * 1000).toLocaleString() : 'never' }}</template>
        </el-table-column>
        <el-table-column label="Created" width="170">
          <template #default="{ row }">{{ new Date(row.created_at * 1000).toLocaleString() }}</template>
        </el-table-column>
        <el-table-column label="Enabled" width="80">
          <template #default="{ row }"><el-switch :model-value="row.enabled === 1" @change="toggle(row)" /></template>
        </el-table-column>
        <el-table-column label="Actions" width="100" fixed="right">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !tokens.length" description='No tokens yet. Click "Create Token" to issue one.' />
    </el-card>

    <el-dialog v-model="dialog.visible" title="Create Token" width="420px">
      <el-form label-width="90px">
        <el-form-item label="Name"><el-input v-model="dialog.name" placeholder="e.g. laptop / ci" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="create">Create</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="reveal.visible" title="Your Token" width="520px">
      <el-alert type="warning" title="Copy this token now. It is shown only once." :closable="false" class="mb" />
      <el-input :model-value="reveal.token" readonly>
        <template #append><el-button @click="copyReveal">Copy</el-button></template>
      </el-input>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.mb { margin-bottom: 10px; }
.ml { margin-left: 8px; }
.base { background: #f0f2f5; border-radius: 4px; padding: 1px 8px; color: #1677ff; }
.key { font-size: 12px; background: #f0f2f5; border-radius: 4px; padding: 1px 8px; }
</style>
