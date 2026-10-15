<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi } from '../../api'
import type { UserChannel, UserKey } from '../../api'

const keys = ref<UserKey[]>([])
const channels = ref<UserChannel[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    keys.value = await portalApi.keys()
    channels.value = await portalApi.channels()
  } finally {
    loading.value = false
  }
}

const dialog = reactive({
  visible: false, channel_id: 0, name: '', key: '', source: '', remark: '',
  mode: 'single' as 'single' | 'bulk', bulkText: '',
})

function openAdd() {
  Object.assign(dialog, {
    visible: true, channel_id: channels.value[0]?.id ?? 0, name: '', key: '', source: '', remark: '',
    mode: 'single', bulkText: '',
  })
}

// 批量解析：每行 name=key 或 name:key，或仅 key
function parseKeyLines(text: string): Array<{ name: string; key: string }> {
  return text.split('\n').map((l) => l.trim()).filter(Boolean).map((line) => {
    const m = line.match(/^(.+?)\s*[=:]\s*(.+)$/)
    if (m) return { name: m[1].trim(), key: m[2].trim() }
    return { name: '', key: line }
  })
}

async function add() {
  if (!dialog.channel_id) { ElMessage.warning('Please select a channel'); return }
  let entries: Array<{ name?: string; source?: string; remark?: string; key: string }> = []
  if (dialog.mode === 'bulk') {
    entries = parseKeyLines(dialog.bulkText)
    if (!entries.length) { ElMessage.warning('Nothing to add'); return }
  } else {
    if (!dialog.key.trim()) { ElMessage.warning('API key is required'); return }
    entries = [{ name: dialog.name, source: dialog.source, remark: dialog.remark, key: dialog.key.trim() }]
  }
  try {
    const r = await portalApi.addKeys(dialog.channel_id, entries)
    ElMessage.success(`Added ${r.added} key(s)`)
    dialog.visible = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to add key')
  }
}

const editDialog = reactive({ visible: false, id: 0, name: '', source: '', remark: '', key: '', enabled: 1 })

function openEdit(k: UserKey) {
  Object.assign(editDialog, { visible: true, id: k.id, name: k.name, source: k.source, remark: k.remark, key: '', enabled: k.enabled })
}

async function saveEdit() {
  try {
    await portalApi.updateKey(editDialog.id, {
      name: editDialog.name, source: editDialog.source, remark: editDialog.remark,
      enabled: editDialog.enabled, ...(editDialog.key.trim() ? { key: editDialog.key.trim() } : {}),
    })
    ElMessage.success('Saved')
    editDialog.visible = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to save')
  }
}

async function toggle(k: UserKey) {
  const next = k.enabled ? 0 : 1
  try {
    await portalApi.updateKey(k.id, { enabled: next })
    k.enabled = next
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Operation failed')
  }
}

async function remove(k: UserKey) {
  try {
    await ElMessageBox.confirm(`Delete key "${k.name || k.key_tail}" from channel "${k.channel_name}"?`, 'Delete Key', { type: 'warning' })
  } catch { return }
  try {
    await portalApi.removeKey(k.id)
    keys.value = keys.value.filter((x) => x.id !== k.id)
    ElMessage.success('Deleted')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to delete')
  }
}

// 用本人 Key 测试与上游渠道的连通性
const testingId = ref(0)
async function test(k: UserKey) {
  testingId.value = k.id
  try {
    const r = await portalApi.testKey(k.id)
    if (r.ok) ElMessage.success(`Connection OK (${r.latency_ms}ms)`)
    else ElMessage.error(`Connection failed: ${r.error || `status ${r.status}`}`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Test failed')
  } finally {
    testingId.value = 0
  }
}

const hasChannels = computed(() => channels.value.length > 0)

onMounted(load)
</script>

<template>
  <div>
    <el-alert type="info" :closable="false" class="mb">
      Your API keys are private to your account. Gateway traffic is routed only through keys you enter here.
    </el-alert>

    <el-card shadow="never" v-loading="loading">
      <template #header>
        <div class="head">
          <span>My API Keys</span>
          <el-button type="primary" size="small" :disabled="!hasChannels" @click="openAdd">Add Key</el-button>
        </div>
      </template>

      <el-empty v-if="!hasChannels" description="No channel is available. Please contact the administrator." />

      <el-table v-else :data="keys" size="small">
        <el-table-column prop="channel_name" label="Channel" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.channel_name || `#${row.channel_id}` }}</template>
        </el-table-column>
        <el-table-column label="Name" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.name || 'Unnamed' }}</template>
        </el-table-column>
        <el-table-column label="Key" width="150">
          <template #default="{ row }"><code class="key">{{ row.masked }}{{ row.key_tail }}</code></template>
        </el-table-column>
        <el-table-column prop="remark" label="Remark" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '—' }}</template>
        </el-table-column>
        <el-table-column label="Last used" width="160">
          <template #default="{ row }">{{ row.last_used_at ? new Date(row.last_used_at * 1000).toLocaleString() : 'never' }}</template>
        </el-table-column>
        <el-table-column label="Enabled" width="80">
          <template #default="{ row }"><el-switch :model-value="row.enabled === 1" @change="toggle(row)" /></template>
        </el-table-column>
        <el-table-column label="Actions" width="190" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">Edit</el-button>
            <el-button link type="success" size="small" :loading="testingId === row.id" @click="test(row)">Test</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog.visible" title="Add API Key" width="500px">
      <el-form label-width="110px">
        <el-form-item label="Channel" required>
          <el-select v-model="dialog.channel_id" style="width: 100%">
            <el-option v-for="c in channels" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="Mode">
          <el-radio-group v-model="dialog.mode">
            <el-radio-button label="single">Single</el-radio-button>
            <el-radio-button label="bulk">Bulk</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <template v-if="dialog.mode === 'single'">
          <el-form-item label="Name"><el-input v-model="dialog.name" placeholder="e.g. primary" /></el-form-item>
          <el-form-item label="API Key" required>
            <el-input v-model="dialog.key" placeholder="sk-..." show-password />
          </el-form-item>
          <el-form-item label="Source"><el-input v-model="dialog.source" placeholder="e.g. official" /></el-form-item>
          <el-form-item label="Remark"><el-input v-model="dialog.remark" /></el-form-item>
        </template>
        <template v-else>
          <el-form-item label="Keys">
            <el-input v-model="dialog.bulkText" type="textarea" :rows="6" placeholder="name=key, one per line" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="add">Add</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="editDialog.visible" title="Edit API Key" width="480px">
      <el-form label-width="110px">
        <el-form-item label="Name"><el-input v-model="editDialog.name" /></el-form-item>
        <el-form-item label="New Key">
          <el-input v-model="editDialog.key" placeholder="leave empty to keep current" show-password />
        </el-form-item>
        <el-form-item label="Source"><el-input v-model="editDialog.source" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="editDialog.remark" /></el-form-item>
        <el-form-item label="Enabled">
          <el-switch v-model="editDialog.enabled" :active-value="1" :inactive-value="0" />
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
.mb { margin-bottom: 10px; }
.key { font-size: 12px; background: #f0f2f5; border-radius: 4px; padding: 1px 8px; }
</style>
