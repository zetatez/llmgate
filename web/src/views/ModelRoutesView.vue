<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { channelsApi, modelRoutesApi } from '../api'
import type { Channel, ModelRoute } from '../api'

const list = ref<ModelRoute[]>([])
const channels = ref<Channel[]>([])
const loading = ref(false)
const search = ref('')
const expandedKeys = ref<string[]>([])

const dialog = reactive({ visible: false, editing: false, id: 0, display_name: '', channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 })

interface Group {
  display_name: string
  routes: ModelRoute[]
  count: number
  enabledCount: number
  defaultChannel: string
}

const groups = computed<Group[]>(() => {
  const m = new Map<string, ModelRoute[]>()
  for (const r of list.value) {
    if (!m.has(r.display_name)) m.set(r.display_name, [])
    m.get(r.display_name)!.push(r)
  }
  return [...m.entries()].map(([name, routes]) => {
    const enabled = routes.filter((r) => r.effective_enabled === 1)
    return {
      display_name: name,
      routes,
      count: routes.length,
      enabledCount: enabled.length,
      defaultChannel: enabled[0]?.channel_name || routes[0]?.channel_name || '',
    }
  })
})

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return groups.value
  return groups.value.filter((g) =>
    g.display_name.toLowerCase().includes(q) ||
    g.routes.some((r) =>
      (r.upstream_model || '').toLowerCase().includes(q) ||
      (r.channel_name || '').toLowerCase().includes(q)))
})

async function load() {
  loading.value = true
  try {
    const [rs, chs] = await Promise.all([modelRoutesApi.list(), channelsApi.list()])
    list.value = rs
    channels.value = chs
  } finally { loading.value = false }
}

function onRowClick(row: Group) {
  if (expandedKeys.value.includes(row.display_name)) {
    expandedKeys.value = expandedKeys.value.filter((k) => k !== row.display_name)
  } else {
    expandedKeys.value = [...expandedKeys.value, row.display_name]
  }
}

function openCreate(prefillModel = '') {
  Object.assign(dialog, { visible: true, editing: false, id: 0, display_name: prefillModel, channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 })
}

function openEdit(r: ModelRoute) {
  Object.assign(dialog, { visible: true, editing: true, id: r.id, display_name: r.display_name, channel_id: r.channel_id, upstream_model: r.upstream_model, priority: r.priority, weight: r.weight, enabled: r.enabled })
}

async function submit() {
  if (!dialog.display_name.trim() || !dialog.channel_id || !dialog.upstream_model.trim()) {
    ElMessage.warning('Model name, channel and upstream model are required')
    return
  }
  const d = {
    display_name: dialog.display_name.trim(),
    channel_id: dialog.channel_id,
    upstream_model: dialog.upstream_model.trim(),
    priority: +dialog.priority || 0,
    weight: +dialog.weight || 1,
    enabled: dialog.enabled,
  }
  try {
    if (dialog.editing) await modelRoutesApi.update(dialog.id, d)
    else await modelRoutesApi.create(d)
    ElMessage.success('Saved')
    dialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(r: ModelRoute) {
  await ElMessageBox.confirm(`Delete route ${r.display_name} → ${r.channel_name}?`, 'Delete Route', { type: 'warning' })
  try {
    await modelRoutesApi.remove(r.id)
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

onMounted(load)
</script>

<template>
  <div>
    <el-card shadow="never">
      <template #header>
        <div class="head">
          <span>Model Routes</span>
          <div>
            <el-input v-model="search" placeholder="Search model / channel / upstream model" clearable style="width: 280px" class="mr" />
            <el-button type="primary" size="small" @click="openCreate()">Add Route</el-button>
          </div>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="filtered"
        size="small"
        row-key="display_name"
        :expand-row-keys="expandedKeys"
        default-sort="{ prop: 'display_name', order: 'ascending' }"
        @row-click="onRowClick"
      >
        <el-table-column type="expand">
          <template #default="{ row }">
            <el-table :data="row.routes" size="small" class="inner-table" @row-click.stop>
              <el-table-column prop="channel_name" label="Channel" min-width="160" />
              <el-table-column prop="upstream_model" label="Upstream Model" min-width="160" show-overflow-tooltip />
              <el-table-column prop="priority" label="Priority" width="90" />
              <el-table-column prop="weight" label="Weight" width="80" />
              <el-table-column label="Enabled" width="130">
                <template #default="{ row: r }">
                  <el-tooltip :content="r.channel_enabled ? (r.enabled ? 'Active' : 'Route disabled') : 'Channel is disabled — route inactive'">
                    <el-tag :type="r.effective_enabled ? 'success' : 'info'" size="small">
                      {{ r.effective_enabled ? 'Active' : 'Inactive' }}
                    </el-tag>
                  </el-tooltip>
                </template>
              </el-table-column>
              <el-table-column label="Actions" width="130">
                <template #default="{ row: r }">
                  <el-button link type="primary" size="small" @click.stop="openEdit(r)">Edit</el-button>
                  <el-button link type="danger" size="small" @click.stop="remove(r)">Delete</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-button size="small" class="mt add-route-btn" @click.stop="openCreate(row.display_name)">
              + Add another channel for this model
            </el-button>
          </template>
        </el-table-column>

        <el-table-column prop="display_name" label="Model Name" min-width="220" sortable show-overflow-tooltip />
        <el-table-column label="Channels" width="130">
          <template #default="{ row }">
            <el-tag :type="row.count > 1 ? 'warning' : 'info'" size="small">{{ row.count }} channel(s)</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="defaultChannel" label="Primary" min-width="140" show-overflow-tooltip />
        <el-table-column label="Status" width="130">
          <template #default="{ row }">
            <el-tag :type="row.enabledCount > 0 ? 'success' : 'danger'" size="small">
              {{ row.enabledCount > 0 ? `Active ${row.enabledCount}/${row.count}` : 'All inactive' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>

      <el-alert v-if="!filtered.length && !loading" class="mt" title="No matching routes. Model routes are synced automatically — adjust the interval in Settings." type="info" :closable="false" />
    </el-card>

    <el-dialog v-model="dialog.visible" :title="dialog.editing ? 'Edit Route' : 'Add Route'" width="480px">
      <el-form label-width="140px">
        <el-form-item label="Model name" required>
          <el-input v-model="dialog.display_name" placeholder="e.g. deepseek-chat" />
        </el-form-item>
        <el-form-item label="Channel" required>
          <el-select v-model="dialog.channel_id" style="width: 100%">
            <el-option v-for="ch in channels" :key="ch.id" :label="`${ch.name} (${ch.base_url})`" :value="ch.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="Upstream model" required>
          <el-input v-model="dialog.upstream_model" placeholder="Real upstream model name" />
        </el-form-item>
        <el-form-item label="Priority">
          <el-input-number v-model="dialog.priority" :min="-10" :max="10" />
          <span class="tip">lower = preferred</span>
        </el-form-item>
        <el-form-item label="Weight"><el-input-number v-model="dialog.weight" :min="1" /></el-form-item>
        <el-form-item label="Enabled">
          <el-switch v-model="dialog.enabled" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="submit">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.mr { margin-right: 8px; }
.mt { margin-top: 8px; }
.tip { font-size: 12px; color: #909399; margin-left: 8px; }
.inner-table { margin: 4px 16px 0; }
.add-route-btn { margin-left: 16px; }
</style>