<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { logsApi, settingsApi, usersApi } from '../api'
import type { AdminUserDetail, User, UserModelUsage } from '../api'

const list = ref<User[]>([])
const loading = ref(false)

// 网关 base URL（供客户端配置）
const gatewayBase = ref('')
const gatewayPrefix = ref('/')

async function loadGatewayBase() {
  try {
    const items = await settingsApi.get()
    const p = items.find((i) => i.key === 'gateway_prefix')?.val || '/'
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

const dialog = reactive({ visible: false, username: '', password: '', name: '', remark: '', quota_limit: 0 })
const editDialog = reactive({ visible: false, id: 0, username: '', password: '', name: '', remark: '', quota_limit: 0, status: 1 })

async function load() {
  loading.value = true
  try { list.value = await usersApi.list() } finally { loading.value = false }
}

// 打开新增用户弹窗（默认不限额度：0 = 无限）
function openCreateUser() {
  Object.assign(dialog, { visible: true, username: '', password: '', name: '', remark: '', quota_limit: 0 })
}

async function create() {
  if (!dialog.username.trim()) { ElMessage.warning('Username is required'); return }
  if (dialog.password.length < 6) { ElMessage.warning('Password must be at least 6 characters'); return }
  try {
    await usersApi.create({
      name: dialog.name, remark: dialog.remark, username: dialog.username.trim(),
      password: dialog.password, quota_limit: +dialog.quota_limit || 0,
    })
    ElMessage.success('User created')
    dialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to create') }
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
  Object.assign(editDialog, { visible: true, id: u.id, username: u.username, password: '', name: u.name, remark: u.remark, quota_limit: u.quota_limit, status: u.status })
}

async function saveEdit() {
  const payload: Record<string, unknown> = {
    username: editDialog.username, name: editDialog.name, remark: editDialog.remark,
    quota_limit: +editDialog.quota_limit || 0, status: editDialog.status,
  }
  if (editDialog.password) {
    if (editDialog.password.length < 6) { ElMessage.warning('Password must be at least 6 characters'); return }
    payload.password = editDialog.password
  }
  try {
    await usersApi.update(editDialog.id, payload as any)
    ElMessage.success('Saved')
    editDialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(u: User) {
  await ElMessageBox.confirm(`Delete user "${u.name || u.username}"? Their tokens and contributed API keys will be removed.`, 'Delete User', { type: 'warning' })
  try {
    await usersApi.remove(u.id)
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

// 令牌操作（一人一个，直接在用户行内操作）
async function createUserToken(u: User) {
  try {
    const t = await usersApi.upsertToken(u.id, { expires_in_days: 0 })
    if (t.token) {
      await navigator.clipboard?.writeText(t.token)
      ElMessage.success('Token created & copied')
    } else {
      ElMessage.success('Token created')
    }
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to create token') }
}

async function copyUserToken(u: User) {
  try {
    const t = await usersApi.token(u.id)
    if (t.token) {
      await navigator.clipboard?.writeText(t.token)
      ElMessage.success('Token copied')
    } else {
      ElMessage.warning('No token')
    }
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to load token') }
}

async function rotateUserToken(u: User) {
  try {
    await ElMessageBox.confirm('Renew this token? The old token stops working immediately.', 'Renew Token', { type: 'warning' })
  } catch { return }
  try {
    const t = await usersApi.rotateToken(u.id)
    if (t.token) await navigator.clipboard?.writeText(t.token)
    ElMessage.success('Token renewed & copied')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to renew') }
}

async function revokeUserToken(u: User) {
  try {
    await ElMessageBox.confirm('Revoke this token? Clients using it lose access immediately.', 'Revoke Token', { type: 'warning' })
  } catch { return }
  try {
    await usersApi.removeToken(u.id)
    ElMessage.success('Token revoked')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to revoke') }
}


// 用户详情（Key / 令牌 / 用量）
const details = reactive({ visible: false, loading: false, data: null as AdminUserDetail | null })

async function openDetails(u: User) {
  details.visible = true
  details.loading = true
  details.data = null
  try {
    details.data = await usersApi.overview(u.id)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load details')
    details.visible = false
  } finally {
    details.loading = false
  }
}

function money(v?: number) {
  return (v ?? 0).toFixed(4)
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
          <span />
          <el-button type="primary" size="small" @click="openCreateUser">Add User</el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="list" size="small">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="username" label="Username" width="130" />
        <el-table-column prop="name" label="Name" width="120" />
        <el-table-column prop="remark" label="Remark" min-width="100" show-overflow-tooltip />
        <el-table-column label="Token" min-width="300">
          <template #default="{ row }">
            <div class="token-cell">
              <template v-if="row.has_token">
                <el-tooltip :content="`expires: ${row.token_expires_at ? new Date(row.token_expires_at * 1000).toLocaleString() : 'never'}`" placement="top">
                  <code class="key">sk-****{{ row.token_tail }}</code>
                </el-tooltip>
                <el-button link type="primary" size="small" @click="copyUserToken(row)">Copy</el-button>
                <el-button link type="warning" size="small" @click="rotateUserToken(row)">Renew</el-button>
                <el-button link type="danger" size="small" @click="revokeUserToken(row)">Revoke</el-button>
              </template>
              <el-button v-else link type="primary" size="small" @click="createUserToken(row)">Create</el-button>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="Quota ($)" width="150">
          <template #default="{ row }">
            <span>{{ row.quota_used.toFixed(4) }} / {{ row.quota_limit ? row.quota_limit : 'unlimited' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Status" width="80">
          <template #default="{ row }">
            <el-switch :model-value="row.status === 1" @change="toggleUserStatus(row)" />
          </template>
        </el-table-column>
        <el-table-column label="Actions" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetails(row)">Details</el-button>
            <el-button link type="primary" size="small" @click="openEdit(row)">Edit</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !list.length" description='No users yet. Click "Add User" to create an account.' />
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

    <el-dialog v-model="dialog.visible" title="Add User" width="440px">
      <el-form label-width="120px">
        <el-form-item label="Username" required><el-input v-model="dialog.username" /></el-form-item>
        <el-form-item label="Password" required><el-input v-model="dialog.password" type="password" show-password /></el-form-item>
        <el-form-item label="Name"><el-input v-model="dialog.name" placeholder="display name (optional)" /></el-form-item>
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

    <el-dialog v-model="editDialog.visible" title="Edit User" width="440px">
      <el-form label-width="120px">
        <el-form-item label="Username"><el-input v-model="editDialog.username" /></el-form-item>
        <el-form-item label="New Password"><el-input v-model="editDialog.password" type="password" show-password placeholder="leave empty to keep current" /></el-form-item>
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

    <el-dialog v-model="details.visible" :title="`User · ${details.data?.user.username || ''}`" width="760px">
      <div v-loading="details.loading">
        <template v-if="details.data">
          <el-descriptions :column="3" border size="small" class="mb">
            <el-descriptions-item label="Name">{{ details.data.user.name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Quota ($)">
              {{ money(details.data.user.quota_used) }} / {{ details.data.user.quota_limit ? money(details.data.user.quota_limit) : 'unlimited' }}
            </el-descriptions-item>
            <el-descriptions-item label="Status">
              <el-tag :type="details.data.user.status === 1 ? 'success' : 'info'" size="small">{{ details.data.user.status === 1 ? 'Enabled' : 'Disabled' }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Today req">{{ details.data.today.total_requests }}</el-descriptions-item>
            <el-descriptions-item label="Total req">{{ details.data.total.total_requests }}</el-descriptions-item>
            <el-descriptions-item label="Total cost ($)">{{ money(details.data.total.cost) }}</el-descriptions-item>
          </el-descriptions>

          <el-tabs>
            <el-tab-pane :label="`API Keys (${details.data.keys.length})`">
              <el-table :data="details.data.keys" size="small" max-height="260">
                <el-table-column prop="channel_name" label="Channel" min-width="120" show-overflow-tooltip />
                <el-table-column label="Name" min-width="100"><template #default="{ row }">{{ row.name || 'Unnamed' }}</template></el-table-column>
                <el-table-column label="Key" width="130"><template #default="{ row }"><code class="key">sk-****{{ row.key_tail }}</code></template></el-table-column>
                <el-table-column label="Enabled" width="80"><template #default="{ row }">{{ row.enabled ? 'yes' : 'no' }}</template></el-table-column>
                <el-table-column label="Last used" width="150"><template #default="{ row }">{{ row.last_used_at ? new Date(row.last_used_at * 1000).toLocaleString() : 'never' }}</template></el-table-column>
              </el-table>
              <el-empty v-if="!details.data.keys.length" description="No keys" />
            </el-tab-pane>
            <el-tab-pane label="Token">
              <template v-if="details.data.token.has_token">
                <el-descriptions :column="1" border size="small">
                  <el-descriptions-item label="Token"><code class="key">sk-****{{ details.data.token.token_tail }}</code></el-descriptions-item>
                  <el-descriptions-item label="Expires">{{ details.data.token.expires_at ? new Date(details.data.token.expires_at * 1000).toLocaleString() : 'never' }}</el-descriptions-item>
                  <el-descriptions-item label="Last used">{{ details.data.token.last_used_at ? new Date(details.data.token.last_used_at * 1000).toLocaleString() : 'never' }}</el-descriptions-item>
                </el-descriptions>
              </template>
              <el-empty v-else description="No token" />
            </el-tab-pane>
            <el-tab-pane label="Usage by model (30d)">
              <el-table :data="details.data.by_model" size="small" max-height="260">
                <el-table-column prop="model" label="Model" min-width="140" show-overflow-tooltip />
                <el-table-column prop="requests" label="Requests" width="100" sortable />
                <el-table-column prop="total_tokens" label="Tokens" width="110" sortable />
                <el-table-column label="Cost ($)" width="100" sortable><template #default="{ row }">{{ money(row.cost) }}</template></el-table-column>
              </el-table>
              <el-empty v-if="!details.data.by_model.length" description="No usage" />
            </el-tab-pane>
          </el-tabs>
        </template>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.tip { font-size: 12px; color: var(--ll-muted); margin-left: 8px; }
.mb { margin-bottom: 10px; }
.mt { margin-top: 12px; }
.ml { margin-left: 8px; }
.base { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; color: var(--el-color-primary); }
.key { font-size: 12px; background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.token { display: inline-flex; align-items: center; gap: 8px; }
.token code { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.token-cell { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
.token-cell code { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.token-form { margin-top: 8px; }
.token-add { margin-bottom: 4px; }
</style>
