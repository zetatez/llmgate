<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { channelsApi, modelRoutesApi } from '../api'
import type { Channel, ModelRoute } from '../api'

const list = ref<ModelRoute[]>([])
const channels = ref<Channel[]>([])
const loading = ref(false)
const search = ref('')
const statusFilter = ref<'all' | 'active' | 'inactive'>('all')
const expandedKeys = ref<string[]>([])
const activeTab = ref('synced')

const dialog = reactive({ visible: false, editing: false, id: 0, display_name: '', channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1, custom_price: false, price_input: 0 as number | null, price_output: 0 as number | null })

// 价格展示：保留最多 6 位小数并去掉末尾 0；null → 'auto'
function fmtPrice(v: number | null | undefined): string {
  if (v == null) return 'auto'
  const s = (+v).toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
  return `$${s}`
}

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

// 排序：活跃在前、非活跃在后（保留元素原类型）
function sortActiveFirst<T extends { display_name: string; enabledCount: number }>(list: T[]): T[] {
  return [...list].sort((a, b) => {
    const ad = a.enabledCount > 0 ? 0 : 1
    const bd = b.enabledCount > 0 ? 0 : 1
    if (ad !== bd) return ad - bd
    return a.display_name.localeCompare(b.display_name)
  })
}

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  const out = groups.value.filter((g) => {
    if (statusFilter.value === 'active' && g.enabledCount === 0) return false
    if (statusFilter.value === 'inactive' && g.enabledCount > 0) return false
    if (!q) return true
    return g.display_name.toLowerCase().includes(q) ||
      g.routes.some((r) =>
        (r.upstream_model || '').toLowerCase().includes(q) ||
        (r.channel_name || '').toLowerCase().includes(q))
  })
  return sortActiveFirst(out)
})

// 每个渠道已同步的上游模型名（用于自定义路由/新增路由的搜索下拉）
const upstreamByChannel = computed(() => {
  const m = new Map<number, string[]>()
  for (const r of list.value) {
    if (!m.has(r.channel_id)) m.set(r.channel_id, [])
    m.get(r.channel_id)!.push(r.upstream_model)
  }
  for (const [k, v] of m) m.set(k, [...new Set(v)].sort())
  return m
})

function upstreamOptions(channelId: number): string[] {
  return upstreamByChannel.value.get(channelId) || []
}

// ---------- 自定义路由分组 ----------
// 自定义路由 = 与上游不同名、且不是厂商前缀自动别名（display 不以 "/upstream_model" 结尾）
function isCustomRoute(r: ModelRoute): boolean {
  return r.display_name !== r.upstream_model && !r.display_name.endsWith('/' + r.upstream_model)
}

const customGroups = computed<Array<{ display_name: string; routes: ModelRoute[]; count: number; enabledCount: number }>>(() => {
  const m = new Map<string, ModelRoute[]>()
  for (const r of list.value) {
    if (!isCustomRoute(r)) continue
    if (!m.has(r.display_name)) m.set(r.display_name, [])
    m.get(r.display_name)!.push(r)
  }
  const out = [...m.entries()].map(([name, routes]) => {
    const enabled = routes.filter((r) => r.effective_enabled === 1)
    return { display_name: name, routes, count: routes.length, enabledCount: enabled.length }
  })
  return sortActiveFirst(out)
})

const customDialog = reactive({
  visible: false, editing: false, name: '', originalName: '',
  rows: [] as Array<{ channel_id: number; upstream_model: string; priority: number; weight: number; enabled: number; price_custom: boolean; price_input: number; price_output: number }>,
})

function openCustomCreate() {
  customDialog.visible = true
  customDialog.editing = false
  customDialog.name = ''
  customDialog.originalName = ''
  customDialog.rows = [{ channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1, price_custom: false, price_input: 0, price_output: 0 }]
}

function addCustomRow() {
  customDialog.rows.push({ channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1, price_custom: false, price_input: 0, price_output: 0 })
}

function removeCustomRow(i: number) {
  customDialog.rows.splice(i, 1)
}

async function saveCustomGroup() {
  const name = customDialog.name.trim()
  if (!name) { ElMessage.warning('Custom route name is required'); return }
  const rows = customDialog.rows.filter((r) => r.channel_id > 0 && r.upstream_model.trim())
  if (!rows.length) { ElMessage.warning('Add at least one "channel + upstream model"'); return }
  try {
    // 编辑模式：按"原名字"删除旧路由再重建（用户可能改了名）
    if (customDialog.editing && customDialog.originalName) {
      const old = customGroups.value.find((g) => g.display_name === customDialog.originalName)?.routes ?? []
      for (const r of old) {
        await modelRoutesApi.remove(r.id)
      }
    }
    for (const row of rows) {
      await modelRoutesApi.create({
        display_name: name, channel_id: row.channel_id,
        upstream_model: row.upstream_model.trim(), priority: +row.priority || 0,
        weight: +row.weight || 1, enabled: row.enabled,
        price_input: row.price_custom ? (+row.price_input || 0) : null,
        price_output: row.price_custom ? (+row.price_output || 0) : null,
      })
    }
    ElMessage.success(`Saved "${name}" with ${rows.length} upstream mapping(s)`)
    customDialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Save failed') }
}

function openCustomEdit(g: { display_name: string; routes: ModelRoute[] }) {
  customDialog.editing = true
  customDialog.name = g.display_name
  customDialog.originalName = g.display_name
  customDialog.rows = g.routes.map((r) => ({
    channel_id: r.channel_id, upstream_model: r.upstream_model, priority: r.priority, weight: r.weight, enabled: r.enabled,
    price_custom: r.price_input != null || r.price_output != null,
    price_input: r.price_input ?? 0,
    price_output: r.price_output ?? 0,
  }))
  customDialog.visible = true
}

async function deleteCustomMapping(r: ModelRoute) {
  await ElMessageBox.confirm(`Delete mapping ${r.channel_name} → ${r.upstream_model}?`, 'Delete mapping', { type: 'warning' })
  try { await modelRoutesApi.remove(r.id); ElMessage.success('Deleted'); load() }
  catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Delete failed') }
}

async function deleteCustomGroup(g: { display_name: string; routes: ModelRoute[] }) {
  await ElMessageBox.confirm(`Delete the whole custom route "${g.display_name}"? This removes all its ${g.routes.length} mapping(s).`, 'Delete custom route', { type: 'warning' })
  try {
    for (const r of g.routes) await modelRoutesApi.remove(r.id)
    ElMessage.success('Deleted')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Delete failed') }
}

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
  Object.assign(dialog, { visible: true, editing: false, id: 0, display_name: prefillModel, channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1, custom_price: false, price_input: 0, price_output: 0 })
}

function openEdit(r: ModelRoute) {
  Object.assign(dialog, {
    visible: true, editing: true, id: r.id, display_name: r.display_name, channel_id: r.channel_id, upstream_model: r.upstream_model,
    priority: r.priority, weight: r.weight, enabled: r.enabled,
    custom_price: r.price_input != null || r.price_output != null,
    price_input: r.price_input ?? 0,
    price_output: r.price_output ?? 0,
  })
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
    price_input: dialog.custom_price ? (+(dialog.price_input ?? 0) || 0) : null,
    price_output: dialog.custom_price ? (+(dialog.price_output ?? 0) || 0) : null,
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
    <el-tabs v-model="activeTab">
      <!-- Tab1：自动同步的模型路由 -->
      <el-tab-pane label="Synced Models" name="synced">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Model Routes</span>
              <div>
                <el-input v-model="search" placeholder="Search model / channel / upstream model" clearable style="width: 260px" class="mr" />
                <el-radio-group v-model="statusFilter" size="small" class="mr">
                  <el-radio-button value="all">All</el-radio-button>
                  <el-radio-button value="active">Active</el-radio-button>
                  <el-radio-button value="inactive">Inactive</el-radio-button>
                </el-radio-group>
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
              <el-table-column label="Price in/out ($/1M)" width="150" show-overflow-tooltip>
                <template #default="{ row: r }">
                  <span v-if="r.price_input == null && r.price_output == null" class="tip">auto (global)</span>
                  <span v-else>{{ fmtPrice(r.price_input) }} / {{ fmtPrice(r.price_output) }}</span>
                </template>
              </el-table-column>
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
      <el-alert v-if="statusFilter === 'inactive' && filtered.length && !loading" class="mt" title="These models have no active channel (channel disabled/cooldown, or route disabled). Enable the corresponding channel or route to restore them." type="warning" :closable="false" />
    </el-card>
      </el-tab-pane>

      <!-- Tab2：自定义路由分组（一个名字挂多个渠道×上游） -->
      <el-tab-pane label="Custom Routes" name="custom">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Custom Routes</span>
              <el-button type="primary" size="small" @click="openCustomCreate">New Custom Route</el-button>
            </div>
          </template>
          <el-table v-loading="loading" :data="customGroups" size="small" row-key="display_name">
            <el-table-column type="expand">
              <template #default="{ row }">
                <el-table :data="row.routes" size="small" class="inner-table">
                  <el-table-column prop="channel_name" label="Channel" min-width="140" />
                  <el-table-column prop="upstream_model" label="Upstream Model" min-width="150" show-overflow-tooltip />
                  <el-table-column prop="priority" label="Priority" width="80" />
                  <el-table-column prop="weight" label="Weight" width="70" />
                  <el-table-column label="Price in/out ($/1M)" width="140" show-overflow-tooltip>
                    <template #default="{ row: r }">
                      <span v-if="r.price_input == null && r.price_output == null" class="tip">auto</span>
                      <span v-else>{{ fmtPrice(r.price_input) }} / {{ fmtPrice(r.price_output) }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="Enabled" width="90">
                    <template #default="{ row: r }">
                      <el-tag :type="r.effective_enabled ? 'success' : 'info'" size="small">{{ r.effective_enabled ? 'Active' : 'Inactive' }}</el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column label="Actions" width="110">
                    <template #default="{ row: r }">
                      <el-button link type="danger" size="small" @click="deleteCustomMapping(r)">Delete</el-button>
                    </template>
                  </el-table-column>
                </el-table>
              </template>
            </el-table-column>
            <el-table-column prop="display_name" label="External name" min-width="160" />
            <el-table-column label="Mappings / Active" width="150">
              <template #default="{ row }">
                <el-tag :type="row.enabledCount > 0 ? 'success' : 'danger'" size="small">{{ row.enabledCount }}/{{ row.count }} active</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Default upstream" min-width="170" show-overflow-tooltip>
              <template #default="{ row }">
                {{ row.routes[0]?.channel_name }} → {{ row.routes[0]?.upstream_model }}
              </template>
            </el-table-column>
            <el-table-column label="Actions" width="150" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openCustomEdit(row)">Edit</el-button>
                <el-button link type="danger" size="small" @click="deleteCustomGroup(row)">Delete Group</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-alert v-if="!customGroups.length && !loading" class="mt" title='No custom routes yet. Click "New Custom Route" in the top-right, e.g. define "flash" with deepseek-v4-flash from zenmux and glm-5.3-flash from opencode as its multiple upstreams.' type="info" :closable="false" />
          <p class="tip-line">Multiple upstreams are assigned automatically by priority order + weight ratio; when one is unavailable (rate-limited / out of quota / expired), traffic switches to another mapping automatically. Avoid naming a route the same as an upstream model, so it won't be overwritten by auto-sync.</p>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 自定义路由：新增/编辑 -->
    <el-dialog v-model="customDialog.visible" :title="customDialog.editing ? 'Edit Custom Route' : 'New Custom Route'" width="900px">
      <el-form label-width="130px">
        <el-form-item label="External route name" required>
          <el-input v-model="customDialog.name" placeholder="e.g. flash / my-agent-model" />
        </el-form-item>
        <el-form-item label="Upstream mappings">
          <div v-for="(row, i) in customDialog.rows" :key="i" class="row-block">
            <div class="row-line">
              <el-select v-model="row.channel_id" style="width: 170px" placeholder="Channel" @change="row.upstream_model = ''">
                <el-option v-for="ch in channels" :key="ch.id" :label="ch.name" :value="ch.id" />
              </el-select>
              <el-select
                v-model="row.upstream_model"
                style="width: 190px"
                filterable
                allow-create
                default-first-option
                placeholder="Search upstream model or type directly"
                :no-data-text="'No synced models for this channel yet; type one manually'"
              >
                <el-option v-for="m in upstreamOptions(row.channel_id)" :key="m" :label="m" :value="m" />
              </el-select>
              <el-input-number v-model="row.priority" :min="-10" :max="10" title="Priority (lower = first)" size="small" />
              <el-input-number v-model="row.weight" :min="1" title="Weight" size="small" />
              <el-switch v-model="row.enabled" :active-value="1" :inactive-value="0" />
              <el-button v-if="customDialog.rows.length > 1" link type="danger" size="small" @click="removeCustomRow(i)">Del</el-button>
            </div>
            <div class="row-line price-line">
              <el-checkbox v-model="row.price_custom" size="small">Custom price ($/1M tokens)</el-checkbox>
              <template v-if="row.price_custom">
                <el-input-number v-model="row.price_input" :min="0" :precision="6" :step="0.05" size="small" placeholder="Input price" title="Input price $/1M" />
                <el-input-number v-model="row.price_output" :min="0" :precision="6" :step="0.1" size="small" placeholder="Output price" title="Output price $/1M" />
              </template>
              <span v-else class="tip">When off, this mapping is billed by global model_pricing</span>
            </div>
          </div>
          <el-button size="small" class="mt" @click="addCustomRow">+ Add an upstream mapping</el-button>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="customDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="saveCustomGroup">Save</el-button>
      </template>
    </el-dialog>

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
          <el-select
            v-model="dialog.upstream_model"
            style="width: 100%"
            filterable
            allow-create
            default-first-option
            placeholder="Search upstream model or type directly"
            :no-data-text="'No synced models for this channel yet; type one manually'"
          >
            <el-option v-for="m in upstreamOptions(dialog.channel_id)" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>
        <el-form-item label="Priority">
          <el-input-number v-model="dialog.priority" :min="-10" :max="10" />
          <span class="tip">lower = preferred</span>
        </el-form-item>
        <el-form-item label="Weight"><el-input-number v-model="dialog.weight" :min="1" /></el-form-item>
        <el-form-item label="Enabled">
          <el-switch v-model="dialog.enabled" :active-value="1" :inactive-value="0" />
        </el-form-item>
        <el-form-item label="Custom price">
          <el-switch v-model="dialog.custom_price" />
          <span class="tip">Billed at this route's price when on; falls back to global model_pricing when off</span>
        </el-form-item>
        <template v-if="dialog.custom_price">
          <el-form-item label="Input price $/1M">
            <el-input-number v-model="dialog.price_input" :min="0" :precision="6" :step="0.05" style="width: 100%" />
          </el-form-item>
          <el-form-item label="Output price $/1M">
            <el-input-number v-model="dialog.price_output" :min="0" :precision="6" :step="0.1" style="width: 100%" />
          </el-form-item>
        </template>
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
.row-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 8px; }
.row-line .el-select { flex-shrink: 0; }
.row-block { margin-bottom: 10px; }
.price-line { margin-top: -2px; padding-left: 130px; flex-wrap: wrap; }
.tip-line { font-size: 12px; color: #909399; margin: 12px 4px 0; line-height: 1.6; }
</style>