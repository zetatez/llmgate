<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi } from '../../api'
import type { UserChannel } from '../../api'

const list = ref<UserChannel[]>([])
const loading = ref(false)

const dialog = reactive({ visible: false, editing: false, id: 0, name: '', base_url: '', adapter: 'openai', note: '' })

async function load() {
  loading.value = true
  try { list.value = await portalApi.channels() } finally { loading.value = false }
}

function openCreate() {
  Object.assign(dialog, { visible: true, editing: false, id: 0, name: '', base_url: '', adapter: 'openai', note: '' })
}
function openEdit(c: UserChannel) {
  Object.assign(dialog, { visible: true, editing: true, id: c.id, name: c.name, base_url: c.base_url, adapter: c.adapter, note: c.note })
}

async function save() {
  if (!dialog.name.trim() || !dialog.base_url.trim()) { ElMessage.warning('Name and Base URL are required'); return }
  try {
    if (dialog.editing) {
      await portalApi.updateChannel(dialog.id, { name: dialog.name, base_url: dialog.base_url, adapter: dialog.adapter, note: dialog.note })
    } else {
      await portalApi.createChannel({ name: dialog.name, base_url: dialog.base_url, adapter: dialog.adapter, note: dialog.note })
    }
    ElMessage.success('Saved')
    dialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(c: UserChannel) {
  try {
    await ElMessageBox.confirm(`Delete your channel "${c.name}"? Its keys and routes will be removed.`, 'Delete Channel', { type: 'warning' })
  } catch { return }
  try {
    await portalApi.removeChannel(c.id)
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="head">
        <span />
        <el-button type="primary" size="small" @click="openCreate">New Channel</el-button>
      </div>
    </template>
    <el-table v-loading="loading" :data="list" size="small">
      <el-table-column prop="name" label="Channel" min-width="150" show-overflow-tooltip />
      <el-table-column prop="adapter" label="Adapter" width="100" />
      <el-table-column prop="base_url" label="Base URL" min-width="200" show-overflow-tooltip />
      <el-table-column label="Owner" width="130" show-overflow-tooltip>
        <template #default="{ row }">
          <el-tag v-if="row.is_mine" type="success" size="small" effect="light">You</el-tag>
          <span v-else>{{ row.owner_name || 'Admin' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="My keys" width="90" align="center">
        <template #default="{ row }">{{ row.my_keys }}</template>
      </el-table-column>
      <el-table-column label="Status" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.enabled !== 1" type="info" size="small">Disabled</el-tag>
          <el-tag v-else :type="row.health_state === 'cooldown' ? 'danger' : 'success'" size="small" effect="light">
            {{ row.health_state === 'cooldown' ? 'Cooldown' : 'Healthy' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Actions" width="130" fixed="right">
        <template #default="{ row }">
          <template v-if="row.is_mine">
            <el-button link type="primary" size="small" @click="openEdit(row)">Edit</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
          </template>
          <span v-else class="dim">read-only</span>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && !list.length" description="No channels yet. Click New Channel to register your own." />
  </el-card>

  <el-dialog v-model="dialog.visible" :title="dialog.editing ? 'Edit Channel' : 'New Channel'" width="520px">
    <el-form label-width="110px">
      <el-form-item label="Name" required><el-input v-model="dialog.name" placeholder="e.g. My DeepSeek plan" /></el-form-item>
      <el-form-item label="Base URL" required><el-input v-model="dialog.base_url" placeholder="https://api.deepseek.com or .../v1" /></el-form-item>
      <el-form-item label="Adapter"><el-select v-model="dialog.adapter" style="width: 100%"><el-option label="OpenAI compatible" value="openai" /></el-select></el-form-item>
      <el-form-item label="Note"><el-input v-model="dialog.note" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialog.visible = false">Cancel</el-button>
      <el-button type="primary" @click="save">Save</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.dim { color: var(--ll-muted); font-size: 12px; }
</style>
