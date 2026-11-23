<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as echarts from 'echarts'
import { gatewayApi, logsApi, portalApi, usersApi } from '../api'
import type { AdminUserDetail, MemberStat, User, UserModelUsage } from '../api'
import { fmtDate, fmtDateTime } from '../utils/format'
import { useCopy } from '../utils/copy'
import { PALETTE, AXIS_TOOLTIP } from '../utils/chart'
import UsageSummary from '../components/UsageSummary.vue'

// admin：账号管理 + 用量/账单；user：成员/账单视图，仅本人令牌操作
const props = defineProps<{ scope?: 'admin' | 'user' }>()
const isUser = computed(() => props.scope === 'user')

// 统一行模型：账号（users / members）与区间用量（by-user-model）按 id 合并
interface Row {
  id: number
  username: string
  name: string
  remark: string
  status: number
  created_at: number
  calls_spent: number
  calls_earned: number
  calls_balance: number
  in_spent: number
  out_spent: number
  in_served: number
  out_served: number
  debt_floor_calls: number
  has_token: boolean
  token_tail: string
  token_expires_at: number
  requests: number
  prompt: number
  completion: number
  models: UserModelUsage[]
  isMe: boolean
}

const people = ref<Row[]>([])
const usage = ref<UserModelUsage[]>([])
const loading = ref(false)
const days = ref(7)
// lifetime：按 users 表累计计数器展示；range：按所选时间范围从 request_logs 聚合
const mode = ref<'lifetime' | 'range'>('lifetime')
const search = ref('')
// 账号管理表本地分页（仅 admin），与用量表共用同一次列表结果
const peoplePage = ref(1)
const peopleSize = ref(10)
// admin 页级分栏：账号管理与用量各占整页，默认停在 Users；user 只有用量视图
const adminTab = ref<'users' | 'usage'>('users')
const activeTab = computed({
  get: () => (isUser.value ? 'usage' : adminTab.value),
  set: (v) => { if (!isUser.value) adminTab.value = v as 'users' | 'usage' },
})

// 网关 Base URL：当前域名 + 本机全部 IP（供不同网络的客户端选择）
const gatewayURLs = ref<string[]>([])

async function loadGatewayBase() {
  try {
    gatewayURLs.value = isUser.value ? await portalApi.gatewayEndpoints() : await gatewayApi.endpoints()
  } catch { /* keep fallback */ }
}

function humanNum(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return +(n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return +(n / 1e3).toFixed(1) + 'k'
  return String(n)
}

function userRow(u: User, isMe = false): Row {
  return {
    id: u.id, username: u.username, name: u.name, remark: u.remark, status: u.status, created_at: u.created_at,
    calls_spent: u.calls_spent, calls_earned: u.calls_earned, calls_balance: u.calls_balance,
    in_spent: u.in_spent, out_spent: u.out_spent, in_served: u.in_served, out_served: u.out_served,
    debt_floor_calls: u.debt_floor_calls,
    has_token: u.has_token, token_tail: u.token_tail, token_expires_at: u.token_expires_at,
    requests: 0, prompt: 0, completion: 0, models: [], isMe,
  }
}

function memberRow(m: MemberStat): Row {
  return {
    id: m.id, username: m.username, name: m.name, remark: '', status: 1, created_at: m.created_at,
    calls_spent: m.calls_spent, calls_earned: m.calls_earned, calls_balance: m.calls_balance,
    in_spent: m.in_spent, out_spent: m.out_spent, in_served: m.in_served, out_served: m.out_served,
    debt_floor_calls: 0,
    has_token: false, token_tail: '', token_expires_at: 0,
    requests: 0, prompt: 0, completion: 0, models: [], isMe: false,
  }
}

function blankRow(id: number, username: string): Row {
  return {
    id, username, name: '', remark: '', status: 1, created_at: 0,
    calls_spent: 0, calls_earned: 0, calls_balance: 0,
    in_spent: 0, out_spent: 0, in_served: 0, out_served: 0, debt_floor_calls: 0,
    has_token: false, token_tail: '', token_expires_at: 0,
    requests: 0, prompt: 0, completion: 0, models: [], isMe: false,
  }
}

// 一次取数：admin 拿全量账号；user 拿公开成员列表 + 本人 profile（合并令牌信息）
async function loadPeople() {
  peoplePage.value = 1
  if (isUser.value) {
    const [members, me] = await Promise.all([portalApi.members(), portalApi.profile()])
    const rows = members.map(memberRow)
    const self = userRow(me, true)
    const i = rows.findIndex((r) => r.id === self.id)
    if (i >= 0) {
      self.created_at = rows[i].created_at || self.created_at
      self.remark = rows[i].remark
      rows[i] = self
    } else {
      rows.unshift(self)
    }
    people.value = rows
  } else {
    people.value = (await usersApi.list()).map((u) => userRow(u))
  }
}

// 区间用量：admin 用 logsApi，user 用用户端 teamUsageByModel（同一数据源，不重复取列表）
async function loadUsage() {
  usage.value = isUser.value ? await portalApi.teamUsageByModel(days.value) : await logsApi.usageByUserModel(days.value)
  await renderChart()
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadPeople(), loadUsage()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load users')
  } finally {
    loading.value = false
  }
}

const rows = computed<Row[]>(() => {
  const map = new Map<number, Row>()
  for (const p of people.value) map.set(p.id, { ...p, models: [] })
  for (const r of usage.value) {
    let row = map.get(r.user_id)
    if (!row) {
      row = blankRow(r.user_id, r.user_name || '—')
      map.set(r.user_id, row)
    }
    row.requests += r.requests
    row.prompt += r.prompt_tokens
    row.completion += r.completion_tokens
    row.models.push(r)
  }
  // 默认按请求数量倒序（lifetime = 累计调用，range = 区间请求）
  const req = (r: Row) => (mode.value === 'lifetime' ? r.calls_spent : r.requests)
  return [...map.values()].sort((a, b) => req(b) - req(a))
})

const filteredRows = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return rows.value
  return rows.value.filter((r) => r.username.toLowerCase().includes(q) || (r.name || '').toLowerCase().includes(q))
})

// 账号管理区只列账号（不含仅出现在用量里的历史行），与管理表共用同一次 usersApi.list 结果
const filteredPeople = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return people.value
  return people.value.filter((p) =>
    p.username.toLowerCase().includes(q) || (p.name || '').toLowerCase().includes(q) || (p.remark || '').toLowerCase().includes(q))
})

const pagedPeople = computed(() => {
  const start = (peoplePage.value - 1) * peopleSize.value
  return filteredPeople.value.slice(start, start + peopleSize.value)
})

watch(search, () => { peoplePage.value = 1 })
// 列表变短（删除用户）或改每页条数后把页码夹回有效范围
watch([filteredPeople, peopleSize], () => {
  const maxPage = Math.max(1, Math.ceil(filteredPeople.value.length / peopleSize.value))
  if (peoplePage.value > maxPage) peoplePage.value = maxPage
})

const totals = computed(() => {
  const weekAgo = Date.now() / 1000 - 7 * 86400
  return rows.value.reduce((s, u) => ({
    users: s.users + 1,
    joinedThisWeek: s.joinedThisWeek + (u.created_at >= weekAgo ? 1 : 0),
    requests: s.requests + (mode.value === 'lifetime' ? u.calls_spent : u.requests),
    prompt: s.prompt + (mode.value === 'lifetime' ? u.in_spent : u.prompt),
    completion: s.completion + (mode.value === 'lifetime' ? u.out_spent : u.completion),
    callsBalance: s.callsBalance + u.calls_balance,
  }), { users: 0, joinedThisWeek: 0, requests: 0, prompt: 0, completion: 0, callsBalance: 0 })
})

const summaryItems = computed(() => [
  { label: isUser.value ? 'Members' : 'Users', value: String(totals.value.users), sub: `${totals.value.joinedThisWeek} joined this week` },
  { label: 'Requests', value: humanNum(totals.value.requests) },
  { label: 'Upload / Download tokens', value: `${humanNum(totals.value.prompt)} / ${humanNum(totals.value.completion)}` },
  { label: 'Calls balance', value: String(totals.value.callsBalance), neg: totals.value.callsBalance < 0 },
])

// 占比条：区间按 token，lifetime 按累计上传/下载
const amount = (r: Row) => (mode.value === 'lifetime' ? r.in_spent + r.out_spent : r.prompt + r.completion)
const amountTotal = computed(() => rows.value.reduce((s, r) => s + amount(r), 0) || 1)
function sharePercent(r: Row): number {
  return (amount(r) / amountTotal.value) * 100
}

function rowClass({ row }: { row: Row }): string {
  return isUser.value && row.isMe ? 'me-row' : ''
}

function modelRows(r: Row): UserModelUsage[] {
  return [...r.models].sort((a, b) => b.total_tokens - a.total_tokens)
}

// ---------- 区间图表 ----------
const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

const modelAgg = computed(() => {
  const m = new Map<string, { tokens: number; requests: number }>()
  for (const r of usage.value) {
    const c = m.get(r.model) || { tokens: 0, requests: 0 }
    c.tokens += r.total_tokens
    c.requests += r.requests
    m.set(r.model, c)
  }
  return [...m.entries()].map(([model, v]) => ({ model, ...v })).sort((a, b) => b.tokens - a.tokens).slice(0, 12)
})

async function renderChart() {
  await nextTick()
  // admin 的图表在隐藏的用量页里，切到该页时再初始化，避免 0 尺寸画布
  if (mode.value !== 'range' || !chartEl.value || !modelAgg.value.length || (!isUser.value && adminTab.value !== 'usage')) {
    chart?.dispose()
    chart = null
    return
  }
  const data = [...modelAgg.value].reverse()
  chart?.dispose()
  chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: {
      ...AXIS_TOOLTIP,
      formatter: (params: any[]) => {
        const p = params[0]
        const row = data[p.dataIndex]
        return `${p.name}<br/>tokens: ${humanNum(row.tokens)}<br/>requests: ${row.requests}`
      },
    },
    grid: { left: 8, right: 24, top: 8, bottom: 8, containLabel: true },
    xAxis: { type: 'value', axisLabel: { color: '#8b909c', formatter: (v: number) => humanNum(v) }, splitLine: { lineStyle: { color: '#23252c' } } },
    yAxis: { type: 'category', data: data.map((d) => d.model), axisTick: { show: false }, axisLine: { lineStyle: { color: '#2a2c33' } }, axisLabel: { color: '#8b909c', width: 200, overflow: 'truncate' } },
    series: [{
      type: 'bar',
      barMaxWidth: 16,
      data: data.map((d) => d.tokens),
      itemStyle: {
        borderRadius: [0, 3, 3, 0],
        color: (p: any) => PALETTE[p.dataIndex % PALETTE.length],
      },
    }],
  })
  chart.resize()
}

function onResize() {
  chart?.resize()
}

watch(mode, () => renderChart())
watch(adminTab, () => renderChart())

// ---------- 令牌 / 账号管理 ----------
const { copied, copy } = useCopy()

async function copyGatewayBase(url: string) {
  if (await copy(url, 'gw:' + url)) ElMessage.success('Copied')
}

const dialog = reactive({ visible: false, username: '', password: '', confirm: '', name: '', remark: '', debt_floor_calls: -1000 })
const editDialog = reactive({ visible: false, id: 0, username: '', password: '', name: '', remark: '', status: 1, debt_floor_calls: -1000 })

function openCreateUser() {
  Object.assign(dialog, { visible: true, username: '', password: '', confirm: '', name: '', remark: '', debt_floor_calls: -1000 })
}

const createdToken = reactive({ visible: false, username: '', token: '' })

async function create() {
  if (!dialog.username.trim()) { ElMessage.warning('Username is required'); return }
  if (dialog.password.length < 6) { ElMessage.warning('Password must be at least 6 characters'); return }
  if (dialog.password !== dialog.confirm) { ElMessage.warning('Passwords do not match'); return }
  try {
    const u = await usersApi.create({
      name: dialog.name, remark: dialog.remark, username: dialog.username.trim(),
      password: dialog.password,
      debt_floor_calls: Math.round(+dialog.debt_floor_calls || 0),
    })
    ElMessage.success('User created')
    dialog.visible = false
    if (u.token) {
      Object.assign(createdToken, { visible: true, username: u.username, token: u.token })
    }
    await loadPeople()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to create') }
}

async function copyCreatedToken() {
  if (await copy(createdToken.token, 'created')) ElMessage.success('Copied')
}

// 快捷启用/停用用户（仅 admin）
async function toggleUserStatus(r: Row) {
  const next = r.status ? 0 : 1
  if (!next) {
    try {
      await ElMessageBox.confirm(`Disable user "${r.username}"? Their API requests will stop immediately.`, 'Disable user', { type: 'warning' })
    } catch { return }
  }
  try {
    await usersApi.update(r.id, { status: next })
    r.status = next
    ElMessage.success(next ? 'User enabled' : 'User disabled')
  } catch (e: any) { ElMessage.error('Operation failed: ' + (e?.response?.data?.error || e)) }
}

function openEdit(r: Row) {
  Object.assign(editDialog, {
    visible: true, id: r.id, username: r.username, password: '', name: r.name, remark: r.remark,
    status: r.status,
    debt_floor_calls: r.debt_floor_calls ?? -1000,
  })
}

async function saveEdit() {
  const payload: Record<string, unknown> = {
    username: editDialog.username, name: editDialog.name, remark: editDialog.remark,
    status: editDialog.status,
    debt_floor_calls: Math.round(+editDialog.debt_floor_calls || 0),
  }
  if (editDialog.password) {
    if (editDialog.password.length < 6) { ElMessage.warning('Password must be at least 6 characters'); return }
    payload.password = editDialog.password
  }
  try {
    await usersApi.update(editDialog.id, payload as any)
    ElMessage.success('Saved')
    editDialog.visible = false
    await loadPeople()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') }
}

async function remove(r: Row) {
  await ElMessageBox.confirm(`Delete user "${r.name || r.username}"? Their tokens and contributed API keys will be removed.`, 'Delete User', { type: 'warning' })
  try {
    await usersApi.remove(r.id)
    ElMessage.success('Deleted')
    await loadPeople()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to delete') }
}

async function createUserToken(r: Row) {
  try {
    const t = await usersApi.upsertToken(r.id, { expires_in_days: 0 })
    if (t.token) {
      await navigator.clipboard?.writeText(t.token)
      ElMessage.success('Token created & copied')
    } else {
      ElMessage.success('Token created')
    }
    await loadPeople()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to create token') }
}

async function copyUserToken(r: Row) {
  try {
    const t = isUser.value ? await portalApi.token() : await usersApi.token(r.id)
    if (t.token) {
      if (await copy(t.token, 'user:' + r.id)) ElMessage.success('Token copied')
    } else {
      ElMessage.warning('No token')
    }
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to load token') }
}

async function rotateUserToken(r: Row) {
  try {
    await ElMessageBox.confirm('Refresh this API_KEY? The old key stops working immediately.', 'Refresh API_KEY', { type: 'warning' })
  } catch { return }
  try {
    const t = isUser.value ? await portalApi.rotateToken() : await usersApi.rotateToken(r.id)
    if (t.token) await navigator.clipboard?.writeText(t.token)
    ElMessage.success('API_KEY refreshed & copied')
    await loadPeople()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to refresh') }
}

// 用户详情（Key / 令牌 / 用量，仅 admin）
const details = reactive({ visible: false, loading: false, data: null as AdminUserDetail | null })

async function openDetails(r: Row) {
  details.visible = true
  details.loading = true
  details.data = null
  try {
    details.data = await usersApi.overview(r.id)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load details')
    details.visible = false
  } finally {
    details.loading = false
  }
}

onMounted(async () => {
  await Promise.all([loadAll(), loadGatewayBase()])
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
})
</script>

<template>
  <div>
    <!-- 网关接入信息：单行，超长时横向滚动 -->
    <div class="base-line mb">
      <span class="base-label">Base URL</span>
      <div class="base-values">
        <template v-for="(u, i) in gatewayURLs" :key="u">
          <span v-if="i" class="sep">·</span>
          <code class="base">{{ u }}</code>
          <el-button link type="primary" size="small" @click="copyGatewayBase(u)">{{ copied === 'gw:' + u ? 'copied' : 'copy' }}</el-button>
        </template>
        <span v-if="!gatewayURLs.length" class="dim">—</span>
      </div>
    </div>

    <!-- admin：账号管理与用量分为两个页级标签，各自占满页面；user 复用用量页并隐藏标签栏 -->
    <el-tabs v-model="activeTab" class="admin-tabs" :class="{ 'is-user': isUser }">
      <el-tab-pane v-if="!isUser" label="Users" name="users">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Users</span>
              <div class="head-actions">
                <el-input v-model="search" placeholder="Search user" clearable size="small" class="search" />
                <el-button type="primary" size="small" @click="openCreateUser">Add User</el-button>
              </div>
            </div>
          </template>
          <el-table v-loading="loading" :data="pagedPeople" size="small" row-key="id">
            <el-table-column type="index" label="#" width="50" :index="(i: number) => (peoplePage - 1) * peopleSize + i + 1" />
            <el-table-column label="User" min-width="200">
              <template #default="{ row }">
                <div class="u-name">{{ row.username }}</div>
                <div v-if="row.name && row.name !== row.username" class="u-sub">{{ row.name }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="Remark" min-width="120" show-overflow-tooltip />
            <el-table-column label="API_KEY" min-width="240">
              <template #default="{ row }">
                <div v-if="row.has_token" class="token-cell">
                  <el-tooltip :content="`expires: ${fmtDateTime(row.token_expires_at, 'never')}`" placement="top">
                    <code class="key">sk-****{{ row.token_tail }}</code>
                  </el-tooltip>
                  <el-button link type="primary" size="small" @click="copyUserToken(row)">{{ copied === 'user:' + row.id ? 'copied' : 'copy' }}</el-button>
                  <el-button link type="warning" size="small" @click="rotateUserToken(row)">refresh</el-button>
                </div>
                <el-button v-else link type="primary" size="small" @click="createUserToken(row)">Create</el-button>
              </template>
            </el-table-column>
            <el-table-column label="Status" width="90">
              <template #default="{ row }">
                <el-switch :model-value="row.status === 1" @change="toggleUserStatus(row)" />
              </template>
            </el-table-column>
            <el-table-column label="Joined" width="110">
              <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="Actions" width="180" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDetails(row)">Details</el-button>
                <el-button link type="primary" size="small" @click="openEdit(row)">Edit</el-button>
                <el-button link type="danger" size="small" @click="remove(row)">Delete</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="peoplePage"
            v-model:page-size="peopleSize"
            :page-sizes="[10, 20, 50]"
            :total="filteredPeople.length"
            layout="total, sizes, prev, pager, next"
            size="small"
            class="pager"
          />
          <el-empty v-if="!loading && !filteredPeople.length" description="No users yet. Click Add User to create an account." />
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="Token usage" name="usage">

        <!-- 用量：admin 为用户计的令牌/请求；user 为成员/账期视图 -->
        <el-card shadow="never" :class="{ mt: isUser }">
          <template #header>
            <div class="head">
              <span>{{ isUser ? 'Members & usage' : 'Token usage' }}</span>
              <div class="toolbar">
                <el-radio-group v-model="mode" size="small">
                  <el-radio-button value="lifetime">Lifetime</el-radio-button>
                  <el-radio-button value="range">Range</el-radio-button>
                </el-radio-group>
                <el-radio-group v-if="mode === 'range'" v-model="days" size="small" @change="loadUsage">
                  <el-radio-button :value="1">Today</el-radio-button>
                  <el-radio-button :value="7">7 days</el-radio-button>
                  <el-radio-button :value="30">30 days</el-radio-button>
                  <el-radio-button :value="0">All</el-radio-button>
                </el-radio-group>
              </div>
            </div>
          </template>
          <div class="usage-meta">
            <span class="hint">{{ isUser
              ? (mode === 'lifetime' ? 'Lifetime totals from member accounts.' : 'Aggregated from request logs in the selected range.')
              : (mode === 'lifetime' ? 'Per-user totals from user accounts.' : 'Per user, from request logs in the selected range.') }}</span>
            <el-input v-if="isUser" v-model="search" placeholder="Search member" clearable size="small" class="search" />
          </div>
          <!-- 汇总卡片仅用于 user 的成员/账期视图；admin 这里只看每用户数字 -->
          <div v-if="isUser" class="mt">
            <UsageSummary :items="summaryItems" />
          </div>
          <el-table v-loading="loading" :data="filteredRows" size="small" row-key="id" :row-class-name="rowClass">
            <el-table-column v-if="mode === 'range'" type="expand">
              <template #default="{ row }">
                <div class="expand-wrap">
                  <el-table v-if="modelRows(row).length" :data="modelRows(row)" size="small" style="width: 100%">
                    <el-table-column prop="model" label="Model" min-width="200" show-overflow-tooltip />
                    <el-table-column prop="requests" label="Requests" width="100" align="right" />
                    <el-table-column label="Upload / Download" width="200" align="right">
                      <template #default="{ row: m }">{{ humanNum(m.prompt_tokens) }} / {{ humanNum(m.completion_tokens) }}</template>
                    </el-table-column>
                    <el-table-column label="Total" width="110" align="right">
                      <template #default="{ row: m }">{{ humanNum(m.total_tokens) }}</template>
                    </el-table-column>
                  </el-table>
                  <el-empty v-else description="No usage in this range" :image-size="40" />
                </div>
              </template>
            </el-table-column>
            <el-table-column type="index" label="#" width="50" />
            <el-table-column :label="isUser ? 'Member' : 'User'" min-width="200">
              <template #default="{ row }">
                <div class="member">
                  <span class="u">{{ row.username }}</span>
                  <span v-if="isUser && row.isMe" class="you">you</span>
                </div>
                <div v-if="row.name && row.name !== row.username" class="sub">{{ row.name }}</div>
                <div class="bar"><div class="bar-inner" :style="{ width: sharePercent(row) + '%' }"></div></div>
              </template>
            </el-table-column>
            <el-table-column v-if="isUser" label="API_KEY" min-width="200">
              <template #default="{ row }">
                <div v-if="row.has_token && row.isMe" class="token-cell">
                  <el-tooltip :content="`expires: ${fmtDateTime(row.token_expires_at, 'never')}`" placement="top">
                    <code class="key">sk-****{{ row.token_tail }}</code>
                  </el-tooltip>
                  <el-button link type="primary" size="small" @click="copyUserToken(row)">{{ copied === 'user:' + row.id ? 'copied' : 'copy' }}</el-button>
                  <el-button link type="warning" size="small" @click="rotateUserToken(row)">refresh</el-button>
                </div>
                <span v-else class="dim">—</span>
              </template>
            </el-table-column>
            <el-table-column label="Joined" width="110">
              <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="Requests" width="100" align="right">
              <template #default="{ row }">{{ humanNum(mode === 'lifetime' ? row.calls_spent : row.requests) }}</template>
            </el-table-column>
            <el-table-column label="Upload / Download" width="150" align="right">
              <template #default="{ row }">{{ humanNum(mode === 'lifetime' ? row.in_spent : row.prompt) }} / {{ humanNum(mode === 'lifetime' ? row.out_spent : row.completion) }}</template>
            </el-table-column>
            <el-table-column label="Share" width="80" align="right">
              <template #default="{ row }">{{ sharePercent(row).toFixed(1) }}%</template>
            </el-table-column>
            <el-table-column label="Calls balance" width="140" align="right">
              <template #default="{ row }">
                <span :class="{ neg: row.calls_balance < 0 }">{{ row.calls_balance }}</span>
                <div class="sub">spent {{ row.calls_spent }} · earned {{ row.calls_earned }}</div>
              </template>
            </el-table-column>
            <el-table-column label="Overdraft" width="110" align="right">
              <template #default="{ row }">{{ row.debt_floor_calls }} calls</template>
            </el-table-column>
            <el-table-column v-if="isUser" label="Status" width="90">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{ row.status === 1 ? 'Enabled' : 'Disabled' }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!loading && !filteredRows.length" :description="isUser ? 'No members yet' : 'No usage yet'" />
        </el-card>

        <el-card v-if="mode === 'range' && modelAgg.length" shadow="never" class="mt">
          <template #header>
            <div class="head"><span>Top Models</span><span class="hint">tokens in range</span></div>
          </template>
          <div ref="chartEl" style="height: 300px"></div>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="dialog.visible" title="Add User" width="440px">
      <el-form label-width="120px">
        <el-form-item label="Username" required><el-input v-model="dialog.username" /></el-form-item>
        <el-form-item label="Password" required><el-input v-model="dialog.password" type="password" show-password /></el-form-item>
        <el-form-item label="Confirm" required><el-input v-model="dialog.confirm" type="password" show-password /></el-form-item>
        <el-form-item label="Name"><el-input v-model="dialog.name" placeholder="display name (optional)" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="dialog.remark" /></el-form-item>
        <el-form-item label="Overdraft (calls)">
          <el-input-number v-model="dialog.debt_floor_calls" :min="-100000" :max="0" />
          <span class="tip">negative = allowed debt</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="create">Create</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="createdToken.visible" title="User created · API_KEY" width="520px">
      <p class="tip" style="margin:0 0 10px">
        Copy this API_KEY now for <b>{{ createdToken.username }}</b>. It is shown only once.
      </p>
      <div class="token-cell">
        <code class="key" style="word-break:break-all">{{ createdToken.token }}</code>
        <el-button link type="primary" size="small" @click="copyCreatedToken">{{ copied === 'created' ? 'copied' : 'copy' }}</el-button>
      </div>
      <template #footer>
        <el-button type="primary" @click="createdToken.visible = false">Done</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="editDialog.visible" title="Edit User" width="440px">
      <el-form label-width="120px">
        <el-form-item label="Username"><el-input v-model="editDialog.username" /></el-form-item>
        <el-form-item label="New Password"><el-input v-model="editDialog.password" type="password" show-password placeholder="leave empty to keep current" /></el-form-item>
        <el-form-item label="Name"><el-input v-model="editDialog.name" /></el-form-item>
        <el-form-item label="Remark"><el-input v-model="editDialog.remark" /></el-form-item>
        <el-form-item label="Overdraft (calls)">
          <el-input-number v-model="editDialog.debt_floor_calls" :min="-100000" :max="0" />
          <span class="tip">negative = allowed debt</span>
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
            <el-descriptions-item label="Status">
              <el-tag :type="details.data.user.status === 1 ? 'success' : 'info'" size="small">{{ details.data.user.status === 1 ? 'Enabled' : 'Disabled' }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Today req">{{ details.data.today.total_requests }}</el-descriptions-item>
            <el-descriptions-item label="Total req">{{ details.data.total.total_requests }}</el-descriptions-item>
            <el-descriptions-item label="Created">{{ fmtDate(details.data.user.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="Last login">{{ fmtDateTime(details.data.user.last_login_at, 'never') }}</el-descriptions-item>
          </el-descriptions>

          <el-tabs>
            <el-tab-pane :label="`API Keys (${details.data.keys.length})`">
              <el-table :data="details.data.keys" size="small" max-height="260">
                <el-table-column prop="channel_name" label="Channel" min-width="120" show-overflow-tooltip />
                <el-table-column label="Name" min-width="100"><template #default="{ row }">{{ row.name || 'Unnamed' }}</template></el-table-column>
                <el-table-column label="Key" width="130"><template #default="{ row }"><code class="key">sk-****{{ row.key_tail }}</code></template></el-table-column>
                <el-table-column label="Enabled" width="80"><template #default="{ row }">{{ row.enabled ? 'yes' : 'no' }}</template></el-table-column>
                <el-table-column label="Last used" width="150"><template #default="{ row }">{{ fmtDateTime(row.last_used_at, 'never') }}</template></el-table-column>
              </el-table>
              <el-empty v-if="!details.data.keys.length" description="No keys" />
            </el-tab-pane>
            <el-tab-pane label="API_KEY">
              <template v-if="details.data.token.has_token">
                <el-descriptions :column="1" border size="small">
                  <el-descriptions-item label="API_KEY"><code class="key">sk-****{{ details.data.token.token_tail }}</code></el-descriptions-item>
                  <el-descriptions-item label="Expires">{{ fmtDateTime(details.data.token.expires_at, 'never') }}</el-descriptions-item>
                  <el-descriptions-item label="Last used">{{ fmtDateTime(details.data.token.last_used_at, 'never') }}</el-descriptions-item>
                </el-descriptions>
              </template>
              <el-empty v-else description="No token" />
            </el-tab-pane>
            <el-tab-pane label="Usage by model (30d)">
              <el-table :data="details.data.by_model" size="small" max-height="260">
                <el-table-column prop="model" label="Model" min-width="140" show-overflow-tooltip />
                <el-table-column prop="requests" label="Requests" width="100" sortable />
                <el-table-column prop="total_tokens" label="Tokens" width="110" sortable />
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
.head { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px; }
.head-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.usage-meta { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.hint { font-size: 12px; color: var(--ll-muted); }
.base-line { display: flex; align-items: center; gap: 8px; flex-wrap: nowrap; font-size: 12px; color: var(--ll-muted); min-width: 0; }
.base-label { font-weight: 600; color: var(--ll-muted); white-space: nowrap; }
.base-values { display: flex; align-items: center; gap: 4px; flex-wrap: nowrap; overflow-x: auto; white-space: nowrap; min-width: 0; }
.base-values .base { white-space: nowrap; }
.sep { color: var(--ll-muted); }
.dim { color: var(--ll-muted); }
.tip { font-size: 12px; color: var(--ll-muted); margin-left: 8px; }
.mb { margin-bottom: 10px; }
.mt { margin-top: 14px; }
.ml { margin-left: 8px; }
.base { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; color: var(--el-color-primary); }
.key { font-size: 12px; background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.token-cell { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
.token-cell code { background: var(--ll-soft); border-radius: 4px; padding: 1px 8px; }
.u-name { font-weight: 600; color: var(--ll-ink); }
.u-sub { font-size: 12px; color: var(--ll-muted); margin-top: 1px; }
.search { width: 240px; }
.member { display: flex; align-items: center; gap: 8px; }
.u { font-weight: 600; color: var(--ll-ink); }
.you { font-size: 10px; padding: 0 5px; border-radius: 4px; background: rgba(138, 144, 160, .22); color: var(--el-color-primary-dark-2); }
.sub { font-size: 11px; color: var(--ll-muted); }
.bar { height: 4px; border-radius: 2px; background: var(--ll-soft); margin-top: 5px; overflow: hidden; max-width: 260px; }
.bar-inner { height: 100%; border-radius: 2px; background: linear-gradient(90deg, #a8aebc, #6b7280); }
.neg { color: #c9808a; }
.expand-wrap { padding: 8px 16px 12px; }
.admin-tabs { --el-tabs-header-height: 32px; }
.admin-tabs :deep(.el-tabs__header) { margin-bottom: 10px; }
.admin-tabs :deep(.el-tabs__item) { font-size: 13px; padding: 0 12px; }
.admin-tabs.is-user :deep(.el-tabs__header) { display: none; }
:deep(.me-row) { background: rgba(138, 144, 160, .10); }
</style>
