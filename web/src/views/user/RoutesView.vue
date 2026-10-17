<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi } from '../../api'
import type { UserChannel, UserRoute } from '../../api'
import { vendorOf } from '../../utils/vendor'

const routes = ref<UserRoute[]>([])
const channels = ref<UserChannel[]>([])
const loading = ref(false)
const search = ref('')
const vendor = ref('all')

const myChannels = computed(() => channels.value.filter((c) => c.is_mine))

// 自定义路由：显示名与上游不同名，且不是“厂商前缀/上游名”的自动别名
function isCustomRoute(r: UserRoute): boolean {
  return r.display_name !== r.upstream_model && !r.display_name.endsWith('/' + r.upstream_model)
}
function routeVendor(r: UserRoute): string {
  return isCustomRoute(r) ? 'CustomRoutes' : vendorOf(r.display_name)
}

const vendors = computed(() => {
  const m = new Map<string, number>()
  const seen = new Set<string>()
  for (const r of routes.value) {
    // 同一显示名的多个映射只计一次
    if (seen.has(r.display_name)) continue
    seen.add(r.display_name)
    const v = routeVendor(r)
    m.set(v, (m.get(v) || 0) + 1)
  }
  return [...m.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
})

const filteredRoutes = computed(() => {
  const q = search.value.trim().toLowerCase()
  return routes.value.filter((r) => {
    if (vendor.value !== 'all' && routeVendor(r) !== vendor.value) return false
    if (!q) return true
    return r.display_name.toLowerCase().includes(q) ||
      (r.upstream_model || '').toLowerCase().includes(q) ||
      (r.channel_name || '').toLowerCase().includes(q)
  })
})

async function load() {
  loading.value = true
  try {
    routes.value = await portalApi.routes()
    channels.value = await portalApi.channels()
  } finally { loading.value = false }
}

const dialog = reactive({ visible: false, editing: false, id: 0, display_name: '', channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 })

function openCreate() {
  Object.assign(dialog, { visible: true, editing: false, id: 0, display_name: '', channel_id: myChannels.value[0]?.id ?? 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 })
}
function openEdit(r: UserRoute) {
  Object.assign(dialog, { visible: true, editing: true, id: r.id, display_name: r.display_name, channel_id: r.channel_id, upstream_model: r.upstream_model, priority: r.priority, weight: r.weight, enabled: r.enabled })
}

async function save() {
  if (!dialog.display_name.trim() || !dialog.upstream_model.trim()) { ElMessage.warning('Display name and upstream model are required'); return }
  try {
    if (dialog.editing) {
      await portalApi.updateRoute(dialog.id, {
        display_name: dialog.display_name, upstream_model: dialog.upstream_model,
        priority: +dialog.priority || 0, weight: +dialog.weight || 1, enabled: dialog.enabled,
      })
    } else {
      if (!dialog.channel_id) { ElMessage.warning('Select one of your channels'); return }
      await portalApi.createRoute({
        display_name: dialog.display_name, channel_id: dialog.channel_id, upstream_model: dialog.upstream_model,
        priority: +dialog.priority || 0, weight: +dialog.weight || 1, enabled: dialog.enabled,
      })
    }
    ElMessage.success('Saved')
    dialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(r: UserRoute) {
  try {
    await ElMessageBox.confirm(`Delete route "${r.display_name}" → ${r.channel_name}/${r.upstream_model}?`, 'Delete Route', { type: 'warning' })
  } catch { return }
  try {
    await portalApi.removeRoute(r.id)
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
        <el-button type="primary" size="small" :disabled="!myChannels.length" @click="openCreate">New Route</el-button>
      </div>
    </template>
    <el-alert v-if="!myChannels.length" type="info" :closable="false" class="mb" title="Create a channel first — routes are added under your own channels." />
    <div class="route-toolbar">
      <el-check-tag :checked="vendor === 'all'" @change="vendor = 'all'">All</el-check-tag>
      <el-check-tag v-for="v in vendors" :key="v.name" :checked="vendor === v.name" @change="vendor = v.name">
        {{ v.name }}<span class="vendor-count">{{ v.count }}</span>
      </el-check-tag>
      <el-input v-model="search" placeholder="Search model / channel / upstream" clearable size="small" class="route-search" />
    </div>
    <el-table v-loading="loading" :data="filteredRoutes" size="small">
      <el-table-column prop="display_name" label="Model Name" min-width="170" show-overflow-tooltip />
      <el-table-column prop="channel_name" label="Channel" min-width="140" show-overflow-tooltip />
      <el-table-column prop="upstream_model" label="Upstream Model" min-width="160" show-overflow-tooltip />
      <el-table-column prop="priority" label="Priority" width="90" align="right" />
      <el-table-column prop="weight" label="Weight" width="80" align="right" />
      <el-table-column label="Status" width="120">
        <template #default="{ row }">
          <el-tag :type="row.effective_enabled ? 'success' : 'info'" size="small" effect="light">{{ row.effective_enabled ? 'Active' : 'Inactive' }}</el-tag>
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
    <el-empty v-if="!loading && !routes.length" description="No routes yet" />
    <el-empty v-else-if="!loading && !filteredRoutes.length" description="No routes match the current filter" />
  </el-card>

  <el-dialog v-model="dialog.visible" :title="dialog.editing ? 'Edit Route' : 'New Route'" width="520px">
    <el-form label-width="120px">
      <el-form-item label="Model name" required><el-input v-model="dialog.display_name" placeholder="e.g. my-flash" /></el-form-item>
      <el-form-item v-if="!dialog.editing" label="Channel" required>
        <el-select v-model="dialog.channel_id" style="width: 100%">
          <el-option v-for="c in myChannels" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="Upstream model" required><el-input v-model="dialog.upstream_model" placeholder="e.g. deepseek-chat" /></el-form-item>
      <el-form-item label="Priority"><el-input-number v-model="dialog.priority" :min="-10" :max="10" /><span class="tip">lower = preferred</span></el-form-item>
      <el-form-item label="Weight"><el-input-number v-model="dialog.weight" :min="1" /></el-form-item>
      <el-form-item label="Enabled"><el-switch v-model="dialog.enabled" :active-value="1" :inactive-value="0" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialog.visible = false">Cancel</el-button>
      <el-button type="primary" @click="save">Save</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.route-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 10px; }
.route-toolbar :deep(.el-check-tag) { font-size: 11px; padding: 2px 7px; line-height: 18px; }
.vendor-count { margin-left: 4px; font-size: 10px; opacity: .7; }
.route-search { width: 260px; margin-left: auto; }
.head { display: flex; justify-content: space-between; align-items: center; }
.dim { color: var(--ll-muted); font-size: 12px; }
.tip { font-size: 12px; color: var(--ll-muted); margin-left: 8px; }
.mb { margin-bottom: 12px; }
</style>
