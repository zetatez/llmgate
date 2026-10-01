<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { logsApi, settingsApi, usersApi } from '../api'
import type { User, UserModelUsage } from '../api'

const list = ref<User[]>([])
const loading = ref(false)

// 网关 base URL（供客户端配置）
const gatewayBase = ref('')
const gatewayPrefix = ref('/')

async function loadGatewayBase() {
  try {
    const items = await settingsApi.get()
    const p = items.find((i) => i.key === 'gateway_prefix')?.value || '/'
    gatewayPrefix.value = p === '/' ? '' : p
    const origin = window.location.origin
    gatewayBase.value = `${origin}${gatewayPrefix.value}/v1`
  } catch { /* keep fallback */ }
}

function copyGatewayBase() {
  navigator.clipboard?.writeText(gatewayBase.value)
  ElMessage.success('Copied')
}

// 用户×模型用量
const usage = ref<UserModelUsage[]>([])
const usageDays = ref(7)
const usageLoading = ref(false)

async function loadUsage() {
  usageLoading.value = true
  try {
    usage.value = await logsApi.usageByUserModel(usageDays.value)
  } catch { usage.value = [] } finally { usageLoading.value = false }
}

const dialog = reactive({ visible: false, name: '', remark: '', quota_limit: 10 })
const tokenDialog = reactive({ visible: false, token: '', title: 'Token' })
const editDialog = reactive({ visible: false, id: 0, name: '', remark: '', quota_limit: 10, status: 1 })

async function load() {
  loading.value = true
  try { list.value = await usersApi.list() } finally { loading.value = false }
}

async function create() {
  if (!dialog.name.trim()) { ElMessage.warning('Name is required'); return }
  try {
    const u = await usersApi.create({ name: dialog.name, remark: dialog.remark, quota_limit: +dialog.quota_limit || 0 })
    dialog.visible = false
    tokenDialog.token = u.token || ''
    tokenDialog.title = 'Token'
    tokenDialog.visible = true
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to create') }
}

// 内联复制用户令牌（不弹窗）
async function copyUserToken(u: User) {
  try {
    const r = await usersApi.getToken(u.id)
    await navigator.clipboard.writeText(r.token)
    ElMessage.success(`Token copied${u.name ? ` (${u.name})` : ''}`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load token')
  }
}

async function resetToken(u: User) {
  try {
    await ElMessageBox.confirm(`Reset API key for "${u.name}"? The old key is invalidated and the new one is copied to your clipboard.`, 'Reset API Key', { type: 'warning' })
  } catch { return }
  try {
    const r = await usersApi.resetToken(u.id)
    await navigator.clipboard.writeText(r.token)
    u.token_tail = r.token.slice(-4)
    ElMessage.success('API key reset & copied')
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Reset failed') }
}

// 快捷启用/停用用户
async function toggleUserStatus(u: User) {
  const next = u.status ? 0 : 1
  try {
    await usersApi.update(u.id, { status: next })
    u.status = next
    ElMessage.success(next ? 'User enabled' : 'User disabled')
  } catch (e: any) { ElMessage.error('Operation failed: ' + (e?.response?.data?.error || e)) }
}

function openEdit(u: User) {
  Object.assign(editDialog, { visible: true, id: u.id, name: u.name, remark: u.remark, quota_limit: u.quota_limit, status: u.status })
}

async function saveEdit() {
  try {
    await usersApi.update(editDialog.id, {
      name: editDialog.name, remark: editDialog.remark,
      quota_limit: +editDialog.quota_limit || 0, status: editDialog.status,
    })
    ElMessage.success('Saved')
    editDialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(u: User) {
  await ElMessageBox.confirm(`Delete user "${u.name}"? Their token will be invalidated immediately.`, 'Delete User', { type: 'warning' })
  try {
    await usersApi.remove(u.id)
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

function copyToken() {
  navigator.clipboard?.writeText(tokenDialog.token)
  ElMessage.success('Copied')
}

onMounted(() => {
  load()
  loadUsage()
  loadGatewayBase()
})
</script>

<template>
  <div>
    <!-- 网关接入信息 -->
    <el-alert type="info" :closable="false" class="mb">
      <template #title>
        <span>Base URL: <code class="base">{{ gatewayBase }}</code></span>
        <el-button link type="primary" size="small" class="ml" @click="copyGatewayBase">Copy</el-button>
      </template>
    </el-alert>

    <el-card shadow="never">
      <template #header>
        <div class="head">
          <span>User Tokens</span>
          <el-button type="primary" size="small" @click="dialog.visible = true">Add User</el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="list" size="small">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="Name" width="120" />
        <el-table-column prop="remark" label="Remark" min-width="100" show-overflow-tooltip />
        <el-table-column label="API Key" min-width="200">
          <template #default="{ row }">
            <div class="api-key">
              <code class="key" :class="{ dim: !row.token_tail }">{{ row.token_tail ? `sk-****${row.token_tail}` : '(no token)' }}</code>
              <el-button v-if="row.token_tail" link type="primary" size="small" @click="copyUserToken(row)">Copy</el-button>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="Quota ($)" width="140">
          <template #default="{ row }">
            <span>{{ row.quota_used.toFixed(4) }} / {{ row.quota_limit ? row.quota_limit : 'unlimited' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Status" width="80">
          <template #default="{ row }">
            <el-switch :model-value="row.status === 1" @change="toggleUserStatus(row)" />
          </template>
        </el-table-column>
        <el-table-column label="Actions" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">Edit</el-button>
            <el-button link type="warning" size="small" @click="resetToken(row)">Reset</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" class="mt">
      <template #header>
        <div class="head">
          <span>Usage by User × Model</span>
          <el-select v-model="usageDays" style="width: 120px" @change="loadUsage">
            <el-option label="Today" :value="1" />
            <el-option label="Last 7 days" :value="7" />
            <el-option label="Last 30 days" :value="30" />
            <el-option label="All" :value="0" />
          </el-select>
        </div>
      </template>
      <el-table v-loading="usageLoading" :data="usage" size="small" max-height="420">
        <el-table-column prop="user_name" label="User" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.user_name || `#${row.user_id}` }}</template>
        </el-table-column>
        <el-table-column prop="model" label="Model" min-width="160" show-overflow-tooltip />
        <el-table-column prop="requests" label="Requests" width="90" sortable />
        <el-table-column prop="prompt_tokens" label="Upload tokens" width="110" sortable />
        <el-table-column prop="completion_tokens" label="Download tokens" width="120" sortable />
        <el-table-column prop="total_tokens" label="Total" width="100" sortable />
        <el-table-column prop="cost" label="Cost ($)" width="100" sortable />
      </el-table>
      <el-empty v-if="!usageLoading && !usage.length" description="No usage in the selected period" />
    </el-card>

    <el-dialog v-model="dialog.visible" title="Add User" width="420px">
      <el-form label-width="120px">
        <el-form-item label="Name" required><el-input v-model="dialog.name" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="dialog.remark" /></el-form-item>
        <el-form-item label="Quota limit ($)">
          <el-input-number v-model="dialog.quota_limit" :min="0" :precision="4" />
          <span class="tip">0 = unlimited</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="create">Create</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="tokenDialog.visible" :title="tokenDialog.title" width="500px">
      <el-alert type="warning" title="Copy this token now. Anyone with it can use your gateway quota." :closable="false" class="mb" />
      <el-input :model-value="tokenDialog.token" readonly>
        <template #append>
          <el-button @click="copyToken">Copy</el-button>
        </template>
      </el-input>
    </el-dialog>

    <el-dialog v-model="editDialog.visible" title="Edit User" width="420px">
      <el-form label-width="120px">
        <el-form-item label="Name"><el-input v-model="editDialog.name" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="editDialog.remark" /></el-form-item>
        <el-form-item label="Quota limit ($)">
          <el-input-number v-model="editDialog.quota_limit" :min="0" :precision="4" />
        </el-form-item>
        <el-form-item label="Status">
          <el-switch v-model="editDialog.status" :active-value="1" :inactive-value="0" active-text="Enabled" inactive-text="Disabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="saveEdit">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.tip { font-size: 12px; color: #909399; margin-left: 8px; }
.mb { margin-bottom: 10px; }
.mt { margin-top: 12px; }
.ml { margin-left: 8px; }
.base { background: #f0f2f5; border-radius: 4px; padding: 1px 8px; color: #1677ff; }
.api-key { display: flex; align-items: center; gap: 8px; }
.api-key .key { font-size: 12px; color: #303133; background: #f0f2f5; border-radius: 4px; padding: 1px 8px; }
</style>