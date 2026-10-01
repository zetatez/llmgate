<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { channelsApi, syncApi } from '../api'
import type { Channel, ChannelKeyItem } from '../api'

// ---------- Channel list ----------
const list = ref<Channel[]>([])
const loading = ref(false)
const selected = ref<Channel | null>(null)

const channelDialog = reactive({ visible: false, editing: false, id: 0, name: '', base_url: '', adapter: 'openai', priority: 0, weight: 1, timeout_ms: 60000, enabled: 1, keys: '', note: '' })

async function load() {
  loading.value = true
  try {
    list.value = await channelsApi.list()
    if (selected.value) {
      const fresh = list.value.find((c) => c.id === selected.value!.id)
      selected.value = fresh ?? null
    }
  } finally { loading.value = false }
}

function selectChannel(ch: Channel) {
  selected.value = ch
  keyFilter.value = ''
  loadKeys()
}

function openCreate() {
  Object.assign(channelDialog, { visible: true, editing: false, id: 0, name: '', base_url: '', adapter: 'openai', priority: 0, weight: 1, timeout_ms: 60000, enabled: 1, keys: '', note: '' })
}

function openEdit(ch: Channel) {
  Object.assign(channelDialog, { visible: true, editing: true, id: ch.id, name: ch.name, base_url: ch.base_url, adapter: ch.adapter, priority: ch.priority, weight: ch.weight, timeout_ms: ch.timeout_ms, enabled: ch.enabled, keys: '', note: ch.note })
}

function parseKeyLines(text: string): Array<{ name: string; key: string }> {
  return text.split('\n').map((l) => l.trim()).filter(Boolean).map((line) => {
    const m = line.match(/^(.+?)\s*[=:]\s*(.+)$/)
    if (m) return { name: m[1].trim(), key: m[2].trim() }
    return { name: '', key: line }
  })
}

async function submitChannel() {
  if (!channelDialog.name.trim() || !channelDialog.base_url.trim()) { ElMessage.warning('Name and Base URL are required'); return }
  const d: any = {
    name: channelDialog.name, base_url: channelDialog.base_url, adapter: channelDialog.adapter,
    priority: +channelDialog.priority || 0, weight: +channelDialog.weight || 1, timeout_ms: +channelDialog.timeout_ms || 60000,
    enabled: channelDialog.enabled, note: channelDialog.note,
  }
  const extraKeys = parseKeyLines(channelDialog.keys)
  if (extraKeys.length) d.keys = extraKeys
  try {
    if (channelDialog.editing) await channelsApi.update(channelDialog.id, d)
    else await channelsApi.create(d)
    ElMessage.success('Saved')
    channelDialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function removeChannel(ch: Channel) {
  await ElMessageBox.confirm(`Delete channel "${ch.name}"? Its keys and routes will also be deleted.`, 'Delete Channel', { type: 'warning' })
  try {
    await channelsApi.remove(ch.id)
    if (selected.value?.id === ch.id) selected.value = null
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

// 快捷启用/停用渠道
async function toggleChannel(ch: Channel) {
  const d: any = { enabled: ch.enabled ? 0 : 1 }
  try {
    await channelsApi.update(ch.id, d)
    ch.enabled = d.enabled
    ElMessage.success(d.enabled ? 'Channel enabled' : 'Channel disabled')
  } catch (e: any) { ElMessage.error('Operation failed: ' + (e?.response?.data?.error || e)) }
}

async function testChannel(ch: Channel) {
  try {
    const r = await channelsApi.test(ch.id)
    if (r.ok) ElMessage.success(`Connection OK (${r.latency_ms}ms)`)
    else ElMessage.error(`Connection failed: ${r.error || `status ${r.status}`}`)
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Test failed') }
}

// 立即全局同步一次模型路由（等效定时任务立即执行）
const syncing = ref(false)
async function syncNow() {
  if (syncing.value) return
  syncing.value = true
  try {
    const r = await syncApi.now()
    ElMessage.success(`Synced: +${r.created} new, -${r.removed} stale, ${r.skipped} kept${r.errored ? `, ${r.errored} channel(s) failed` : ''} (${r.cost_ms}ms)`)
    if (r.errored > 0) ElMessage.warning(`${r.errored} channel(s) failed to sync — check channel keys/connectivity`)
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Sync failed') } finally { syncing.value = false }
}

// ---------- Key pool (master-detail) ----------
const keys = ref<ChannelKeyItem[]>([])
const keyFilter = ref('')
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const keysLoading = ref(false)

const filteredKeys = computed(() => {
  const q = keyFilter.value.trim().toLowerCase()
  return keys.value.filter((k) => {
    if (statusFilter.value === 'enabled' && !k.enabled) return false
    if (statusFilter.value === 'disabled' && k.enabled) return false
    if (!q) return true
    return (k.name || '').toLowerCase().includes(q) ||
      (k.remark || '').toLowerCase().includes(q) ||
      (k.key_tail || '').toLowerCase().includes(q)
  })
})

const keyStats = computed(() => {
  const total = keys.value.length
  const enabled = keys.value.filter((k) => k.enabled).length
  return { total, enabled }
})

async function loadKeys() {
  if (!selected.value) return
  keysLoading.value = true
  try { keys.value = await channelsApi.keys(selected.value.id) } finally { keysLoading.value = false }
}

async function toggleKey(k: ChannelKeyItem) {
  const next = k.enabled ? 0 : 1
  try {
    await channelsApi.updateKey(k.id, { enabled: next })
    k.enabled = next
  } catch (e: any) { ElMessage.error('Operation failed: ' + (e?.response?.data?.error || e)) }
}

async function updateKeyWeight(k: ChannelKeyItem, weight: number) {
  try {
    await channelsApi.updateKey(k.id, { weight: +weight || 1 })
    k.weight = +weight || 1
  } catch (e: any) { ElMessage.error('Failed to save weight') }
}

async function removeKey(k: ChannelKeyItem) {
  await ElMessageBox.confirm(`Delete key "${k.name || k.masked}"?`, 'Delete Key', { type: 'warning' })
  try {
    await channelsApi.removeKey(k.id)
    keys.value = keys.value.filter((x) => x.id !== k.id)
    ElMessage.success('Deleted')
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

// Edit account (name/remark, replace secret, enable, weight)
const editKeyDialog = reactive({ visible: false, id: 0, name: '', remark: '', key: '', enabled: 1, weight: 1, busy: false })

function openEditKey(k: ChannelKeyItem) {
  Object.assign(editKeyDialog, {
    visible: true, id: k.id, name: k.name, remark: k.remark, key: '', enabled: k.enabled, weight: k.weight, busy: false,
  })
}

async function submitEditKey() {
  editKeyDialog.busy = true
  try {
    const d: any = { name: editKeyDialog.name, remark: editKeyDialog.remark, enabled: editKeyDialog.enabled, weight: +editKeyDialog.weight || 1 }
    const newKey = editKeyDialog.key.trim()
    if (newKey) d.key = newKey
    await channelsApi.updateKey(editKeyDialog.id, d)
    ElMessage.success(newKey ? 'Saved (secret replaced)' : 'Saved')
    editKeyDialog.visible = false
    await loadKeys()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') } finally { editKeyDialog.busy = false }
}

// Add key
const addDialog = reactive({ visible: false, mode: 'single' as 'single' | 'batch', name: '', remark: '', key: '', batchText: '', busy: false })

function openAdd(mode: 'single' | 'batch' = 'single') {
  Object.assign(addDialog, { visible: true, mode, name: '', remark: '', key: '', batchText: '', busy: false })
}

function resetAddForm() {
  addDialog.name = ''; addDialog.remark = ''; addDialog.key = ''
}

async function submitAdd() {
  if (!selected.value) return
  addDialog.busy = true
  try {
    const entries: Array<{ name?: string; remark?: string; key: string }> = []
    if (addDialog.mode === 'single') {
      if (!addDialog.key.trim()) { ElMessage.warning('Please enter the key'); return }
      entries.push({ name: addDialog.name, remark: addDialog.remark, key: addDialog.key.trim() })
    } else {
      const lines = parseKeyLines(addDialog.batchText)
      if (!lines.length) { ElMessage.warning('Nothing to add'); return }
      entries.push(...lines.map((l) => ({ name: l.name, key: l.key })))
    }
    const r = await channelsApi.addKeys(selected.value.id, entries)
    ElMessage.success(`Added ${r.added} key(s)`)
    if (addDialog.mode === 'single') resetAddForm()
    else addDialog.batchText = ''
    await loadKeys()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to add') } finally { addDialog.busy = false }
}

onMounted(load)
</script>

<template>
  <div>
    <!-- Channel list + key pool below -->
    <el-card shadow="never">
      <template #header>
        <div class="head">
          <span>Channels</span>
          <div>
            <el-button
              size="small"
              class="mr"
              :loading="syncing"
              :disabled="syncing"
              @click="syncNow"
            >
              {{ syncing ? 'Syncing…' : 'Sync All Routes' }}
            </el-button>
            <el-button type="primary" size="small" @click="openCreate">Add Channel</el-button>
          </div>
        </div>
      </template>
      <el-table v-loading="loading" :data="list" size="small" highlight-current-row @current-change="(row: Channel) => row && selectChannel(row)">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="Name" width="150" />
        <el-table-column prop="base_url" label="Base URL" min-width="210" show-overflow-tooltip />
        <el-table-column label="Status" width="70">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled === 1" @change="toggleChannel(row)" />
          </template>
        </el-table-column>
        <el-table-column prop="priority" label="Priority" width="80" />
        <el-table-column prop="weight" label="Weight" width="80" />
        <el-table-column label="Health" width="90">
          <template #default="{ row }">
            <el-tag :type="row.health_state === 'cooldown' ? 'danger' : 'success'" size="small">{{ row.health_state === 'cooldown' ? 'Cooling' : 'OK' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Actions" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click.stop="testChannel(row)">Test</el-button>
            <el-button link type="primary" size="small" @click.stop="openEdit(row)">Edit</el-button>
            <el-button link type="danger" size="small" @click.stop="removeChannel(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- Selected channel key pool -->
    <el-card v-if="selected" shadow="never" class="mt">
      <template #header>
        <div class="head">
          <span>
            Keys · <b>{{ selected.name }}</b>
            <el-tag size="small" class="ml">{{ keyStats.total }} key(s)</el-tag>
            <el-tag v-if="keyStats.enabled < keyStats.total" type="warning" size="small" class="ml">{{ keyStats.enabled }} enabled</el-tag>
          </span>
          <div class="toolbar">
            <el-input v-model="keyFilter" placeholder="Search name/remark/tail" clearable style="width: 200px" class="mr" />
            <el-select v-model="statusFilter" style="width: 110px" class="mr">
              <el-option label="All" value="all" />
              <el-option label="Enabled" value="enabled" />
              <el-option label="Disabled" value="disabled" />
            </el-select>
            <span class="count">showing {{ filteredKeys.length }}/{{ keys.length }}</span>
            <el-button size="small" class="ml" @click="openAdd('batch')">Bulk Import</el-button>
            <el-button type="primary" size="small" @click="openAdd('single')">Add Key</el-button>
          </div>
        </div>
      </template>

      <el-alert
        v-if="selected && selected.enabled !== 1"
        type="warning"
        :closable="false"
        class="mb"
        title="该渠道已停用，其下所有 Key 当前均不参与路由"
      />

      <!-- 行式表格：一次载入全部，表头冻结，支持快速滚动与搜索 -->
      <el-table v-loading="keysLoading" :data="filteredKeys" size="small" height="520" row-key="id">
        <el-table-column type="index" label="#" width="45" />
        <el-table-column label="Name" min-width="130" show-overflow-tooltip>
          <template #default="{ row }"><b :class="{ dim: !row.name }">{{ row.name || 'Unnamed' }}</b></template>
        </el-table-column>
        <el-table-column label="Remark" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '—' }}</template>
        </el-table-column>
        <el-table-column label="Key" width="150">
          <template #default="{ row }"><code :class="{ dim: !row.key_tail }">{{ row.masked }}{{ row.key_tail }}</code></template>
        </el-table-column>
        <el-table-column label="Weight" width="100">
          <template #default="{ row }">
            <el-input-number :model-value="row.weight" :min="1" size="small" controls-position="right" style="width: 90px" @change="(v: any) => updateKeyWeight(row, v)" />
          </template>
        </el-table-column>
        <el-table-column prop="last_used_at" label="Last used" width="140" sortable>
          <template #default="{ row }">{{ row.last_used_at ? new Date(row.last_used_at * 1000).toLocaleString() : 'never' }}</template>
        </el-table-column>
        <el-table-column label="Enabled" width="70" fixed="right">
          <template #default="{ row }"><el-switch :model-value="row.enabled === 1" @change="toggleKey(row)" /></template>
        </el-table-column>
        <el-table-column label="Effective" width="110" fixed="right">
          <template #default="{ row }">
            <el-tooltip :content="row.effective_enabled ? 'Key 已启用且渠道启用，参与路由' : (selected?.enabled ? 'Key 被单独停用' : '渠道已停用，该 Key 当前不参与路由')">
              <el-tag :type="row.effective_enabled ? 'success' : 'info'" size="small">
                {{ row.effective_enabled ? 'Active' : 'Inactive' }}
              </el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="Actions" width="110" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEditKey(row)">Edit</el-button>
            <el-button link type="danger" size="small" @click="removeKey(row)">Remove</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!keysLoading && !keys.length" description='No keys yet. Click "Add Key" to create one.' />
    </el-card>

    <!-- Channel edit / create -->
    <el-dialog v-model="channelDialog.visible" :title="channelDialog.editing ? 'Edit Channel' : 'Add Channel'" width="520px">
      <el-form label-width="110px">
        <el-form-item label="Name" required><el-input v-model="channelDialog.name" /></el-form-item>
        <el-form-item label="Base URL" required><el-input v-model="channelDialog.base_url" placeholder="https://api.deepseek.com or .../v1" /></el-form-item>
        <el-form-item label="Protocol"><el-select v-model="channelDialog.adapter" style="width: 100%"><el-option label="OpenAI compatible" value="openai" /></el-select></el-form-item>
        <el-form-item label="Priority"><el-input-number v-model="channelDialog.priority" :min="-10" :max="10" /><span class="tip">lower = preferred</span></el-form-item>
        <el-form-item label="Weight"><el-input-number v-model="channelDialog.weight" :min="1" /></el-form-item>
        <el-form-item label="Timeout (ms)"><el-input-number v-model="channelDialog.timeout_ms" :min="1000" :step="5000" /></el-form-item>
        <el-form-item label="Enabled"><el-switch v-model="channelDialog.enabled" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item v-if="!channelDialog.editing" label="API Keys">
          <el-input v-model="channelDialog.keys" type="textarea" :rows="3" placeholder="name=key, one per line" />
        </el-form-item>
        <el-form-item label="Note"><el-input v-model="channelDialog.note" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="channelDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="submitChannel">Save</el-button>
      </template>
    </el-dialog>

    <!-- Add key -->
    <el-dialog v-model="addDialog.visible" :title="`Add Key · ${selected?.name ?? ''}`" width="560px">
      <el-tabs v-model="addDialog.mode">
        <el-tab-pane label="Single" name="single">
          <el-form label-width="80px">
            <el-form-item label="Name"><el-input v-model="addDialog.name" placeholder="e.g. main / backup" /></el-form-item>
            <el-form-item label="Remark"><el-input v-model="addDialog.remark" placeholder="e.g. expires end of year" /></el-form-item>
            <el-form-item label="Key" required><el-input v-model="addDialog.key" placeholder="sk-..." /></el-form-item>
          </el-form>
          <el-button type="primary" :loading="addDialog.busy" @click="submitAdd">Add</el-button>
        </el-tab-pane>
        <el-tab-pane label="Bulk" name="batch">
          <el-input v-model="addDialog.batchText" type="textarea" :rows="6" placeholder="name=key, one per line" />
          <el-button type="primary" :loading="addDialog.busy" class="mt" @click="submitAdd">Add All</el-button>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <!-- Edit key -->
    <el-dialog v-model="editKeyDialog.visible" :title="`Edit Key · ${selected?.name ?? ''}`" width="480px">
      <el-form label-width="90px">
        <el-form-item label="Name"><el-input v-model="editKeyDialog.name" placeholder="e.g. main / backup" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="editKeyDialog.remark" placeholder="e.g. expires end of year" /></el-form-item>
        <el-form-item label="Secret">
          <el-input v-model="editKeyDialog.key" type="password" show-password placeholder="Leave empty to keep the current secret" />
        </el-form-item>
        <el-form-item label="Enabled">
          <el-switch v-model="editKeyDialog.enabled" :active-value="1" :inactive-value="0" />
        </el-form-item>
        <el-form-item label="Weight"><el-input-number v-model="editKeyDialog.weight" :min="1" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editKeyDialog.visible = false">Cancel</el-button>
        <el-button type="primary" :loading="editKeyDialog.busy" @click="submitEditKey">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px; }
.toolbar { display: flex; align-items: center; }
.tip { font-size: 12px; color: #909399; margin-left: 8px; }
.mt { margin-top: 12px; }
.mr { margin-right: 8px; }
.ml { margin-left: 6px; }
.mb { margin-bottom: 10px; }
.model-list { display: flex; flex-wrap: wrap; gap: 6px; max-height: 180px; overflow-y: auto; }
.model-tag { background: #f0f2f5; border-radius: 4px; padding: 2px 8px; font-size: 12px; color: #606266; }
.dim { color: #909399; font-weight: normal; }
.count { font-size: 12px; color: #909399; margin-right: 8px; }
</style>