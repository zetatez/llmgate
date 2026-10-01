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
  visible: false, editing: false, name: '',
  rows: [] as Array<{ channel_id: number; upstream_model: string; priority: number; weight: number; enabled: number }>,
})

function openCustomCreate() {
  customDialog.visible = true
  customDialog.editing = false
  customDialog.name = ''
  customDialog.rows = [{ channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 }]
}

function addCustomRow() {
  customDialog.rows.push({ channel_id: 0, upstream_model: '', priority: 0, weight: 1, enabled: 1 })
}

function removeCustomRow(i: number) {
  customDialog.rows.splice(i, 1)
}

async function saveCustomGroup() {
  const name = customDialog.name.trim()
  if (!name) { ElMessage.warning('自定义路由名必填'); return }
  const rows = customDialog.rows.filter((r) => r.channel_id > 0 && r.upstream_model.trim())
  if (!rows.length) { ElMessage.warning('至少填一个「渠道 + 上游模型」'); return }
  try {
    // 编辑模式：先删除旧名全部路由再重建
    if (customDialog.editing) {
      for (const r of customGroups.value.find((g) => g.display_name === name)?.routes ?? []) {
        await modelRoutesApi.remove(r.id)
      }
    }
    for (const row of rows) {
      await modelRoutesApi.create({
        display_name: name, channel_id: row.channel_id,
        upstream_model: row.upstream_model.trim(), priority: +row.priority || 0,
        weight: +row.weight || 1, enabled: row.enabled,
      })
    }
    ElMessage.success(`已保存「${name}」，共 ${rows.length} 条上游映射`)
    customDialog.visible = false
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || '保存失败') }
}

function openCustomEdit(g: { display_name: string; routes: ModelRoute[] }) {
  customDialog.editing = true
  customDialog.name = g.display_name
  customDialog.rows = g.routes.map((r) => ({
    channel_id: r.channel_id, upstream_model: r.upstream_model, priority: r.priority, weight: r.weight, enabled: r.enabled,
  }))
  customDialog.visible = true
}

async function deleteCustomMapping(r: ModelRoute) {
  await ElMessageBox.confirm(`删除映射 ${r.channel_name} → ${r.upstream_model}？`, '删除映射', { type: 'warning' })
  try { await modelRoutesApi.remove(r.id); ElMessage.success('已删除'); load() }
  catch (e: any) { ElMessage.error(e?.response?.data?.error || '删除失败') }
}

async function deleteCustomGroup(g: { display_name: string; routes: ModelRoute[] }) {
  await ElMessageBox.confirm(`删除整个自定义路由「${g.display_name}」？将移除其全部 ${g.routes.length} 条映射。`, '删除自定义路由', { type: 'warning' })
  try {
    for (const r of g.routes) await modelRoutesApi.remove(r.id)
    ElMessage.success('已删除')
    load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || '删除失败') }
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
      <el-alert v-if="statusFilter === 'inactive' && filtered.length && !loading" class="mt" title="这些模型暂无活跃渠道（渠道被停用/冷却，或路由被禁用）。启用对应的渠道或路由后即可恢复。" type="warning" :closable="false" />
    </el-card>
      </el-tab-pane>

      <!-- Tab2：自定义路由分组（一个名字挂多个渠道×上游） -->
      <el-tab-pane label="Custom Routes" name="custom">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>自定义路由（一个对外名 → 多个渠道的上游模型）</span>
              <el-button type="primary" size="small" @click="openCustomCreate">新增自定义路由</el-button>
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
            <el-table-column prop="display_name" label="对外名" min-width="160" />
            <el-table-column label="映射 / 活跃" width="150">
              <template #default="{ row }">
                <el-tag :type="row.enabledCount > 0 ? 'success' : 'danger'" size="small">{{ row.enabledCount }}/{{ row.count }} active</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="默认上游" min-width="170" show-overflow-tooltip>
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
          <el-alert v-if="!customGroups.length && !loading" class="mt" title="还没有自定义路由。点右上角「新增自定义路由」，例如定义 “flash”：把 zenmux 的 deepseek-v4-flash 与 opencode 的 glm-5.3-flash 等作为它的多个上游。" type="info" :closable="false" />
          <p class="tip-line">说明：多个上游会按优先级顺序 + 权重比例自动分配，某条不可用（限流/额度/过期）时自动切换到其它映射。路由名尽量避免与上游模型重名，防止被自动同步覆盖。</p>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 自定义路由：新增/编辑 -->
    <el-dialog v-model="customDialog.visible" :title="customDialog.editing ? '编辑自定义路由' : '新增自定义路由'" width="640px">
      <el-form label-width="130px">
        <el-form-item label="对外路由名" required>
          <el-input v-model="customDialog.name" placeholder="如 flash / my-agent-model" />
        </el-form-item>
        <el-form-item label="上游映射（多行）">
          <div v-for="(row, i) in customDialog.rows" :key="i" class="row-line">
            <el-select v-model="row.channel_id" style="width: 170px" placeholder="渠道" @change="row.upstream_model = ''">
              <el-option v-for="ch in channels" :key="ch.id" :label="ch.name" :value="ch.id" />
            </el-select>
            <el-select
              v-model="row.upstream_model"
              style="width: 190px"
              filterable
              allow-create
              default-first-option
              placeholder="搜索上游模型或直接输入"
              :no-data-text="'该渠道暂无已同步模型，可手动输入'"
            >
              <el-option v-for="m in upstreamOptions(row.channel_id)" :key="m" :label="m" :value="m" />
            </el-select>
            <el-input-number v-model="row.priority" :min="-10" :max="10" title="优先级(小=先)" />
            <el-input-number v-model="row.weight" :min="1" title="权重" />
            <el-switch v-model="row.enabled" :active-value="1" :inactive-value="0" />
            <el-button v-if="customDialog.rows.length > 1" link type="danger" size="small" @click="removeCustomRow(i)">删</el-button>
          </div>
          <el-button size="small" class="mt" @click="addCustomRow">+ 添加一条上游映射</el-button>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="customDialog.visible = false">取消</el-button>
        <el-button type="primary" @click="saveCustomGroup">保存</el-button>
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
            :no-data-text="'该渠道暂无已同步模型，可手动输入'"
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
.row-line { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.tip-line { font-size: 12px; color: #909399; margin: 12px 4px 0; line-height: 1.6; }
</style>