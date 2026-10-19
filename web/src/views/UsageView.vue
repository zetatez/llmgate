<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts'
import { logsApi, portalApi, usersApi } from '../api'
import type { PortalUser, User, UserModelUsage } from '../api'
import { PALETTE, AXIS_TOOLTIP } from '../utils/chart'

const props = defineProps<{ scope?: 'admin' | 'user' }>()
const isUser = computed(() => props.scope === 'user')

const usage = ref<UserModelUsage[]>([])
const users = ref<User[]>([])
const loading = ref(false)
const days = ref(7)
// lifetime：按 users 表累计计数器展示（从注册至今，日志清理后仍保留）
// range：按所选时间范围从 request_logs 聚合（可展开查看每模型明细）
const mode = ref<'lifetime' | 'range'>('lifetime')

function humanNum(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return +(n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return +(n / 1e3).toFixed(1) + 'k'
  return String(n)
}
function money(v: number): string {
  return (v ?? 0).toFixed(4)
}

interface Row {
  user_id: number
  user_name: string
  requests: number
  prompt: number
  completion: number
  cost: number
  in_spent: number
  out_spent: number
  calls_spent: number
  calls_earned: number
  calls_balance: number
  credit_spent: number
  credit_earned: number
  credit_balance: number
  models: UserModelUsage[]
}

const rows = computed<Row[]>(() => {
  const m = new Map<number, Row>()
  for (const u of users.value) {
    m.set(u.id, {
      user_id: u.id, user_name: u.username || u.name,
      requests: 0, prompt: 0, completion: 0, cost: 0,
      in_spent: u.in_spent, out_spent: u.out_spent,
      calls_spent: u.calls_spent, calls_earned: u.calls_earned, calls_balance: u.calls_balance,
      credit_spent: u.credit_spent, credit_earned: u.credit_earned, credit_balance: u.credit_balance,
      models: [],
    })
  }
  for (const r of usage.value) {
    let row = m.get(r.user_id)
    if (!row) {
      row = {
        user_id: r.user_id, user_name: r.user_name || '—',
        requests: 0, prompt: 0, completion: 0, cost: 0,
        in_spent: 0, out_spent: 0,
        calls_spent: 0, calls_earned: 0, calls_balance: 0,
        credit_spent: 0, credit_earned: 0, credit_balance: 0,
        models: [],
      }
      m.set(r.user_id, row)
    }
    row.requests += r.requests
    row.prompt += r.prompt_tokens
    row.completion += r.completion_tokens
    row.cost += r.cost
    row.models.push(r)
  }
  // 默认按请求数量倒序（Lifetime = 累计调用次数，Range = 区间请求数）
  const req = (r: Row) => (mode.value === 'lifetime' ? r.calls_spent : r.requests)
  return [...m.values()].sort((a, b) => req(b) - req(a) || b.cost - a.cost)
})

const totals = computed(() =>
  rows.value.reduce((s, u) => ({
    users: s.users + 1,
    requests: s.requests + (mode.value === 'lifetime' ? u.calls_spent : u.requests),
    prompt: s.prompt + (mode.value === 'lifetime' ? u.in_spent : u.prompt),
    completion: s.completion + (mode.value === 'lifetime' ? u.out_spent : u.completion),
    cost: s.cost + (mode.value === 'lifetime' ? u.credit_spent : u.cost),
    callsBalance: s.callsBalance + u.calls_balance,
    creditBalance: s.creditBalance + u.credit_balance,
  }), { users: 0, requests: 0, prompt: 0, completion: 0, cost: 0, callsBalance: 0, creditBalance: 0 }),
)

// 普通用户：复用同一套 Billing 视图，仅展示自己的数据
function selfToUser(m: PortalUser): User {
  return {
    id: m.id, name: m.name, remark: '', username: m.username,
    quota_limit: m.quota_limit, quota_used: m.quota_used, status: m.status,
    has_token: true, token_tail: '', token_expires_at: 0, token_last_used_at: 0,
    calls_spent: m.calls_spent, calls_earned: m.calls_earned, calls_balance: m.calls_balance,
    in_spent: m.in_spent, out_spent: m.out_spent, in_served: m.in_served, out_served: m.out_served,
    credit_spent: m.credit_spent, credit_earned: m.credit_earned, credit_balance: m.credit_balance,
    debt_floor_calls: m.debt_floor_calls, debt_floor_credit: m.debt_floor_credit,
    last_login_at: m.last_login_at, created_at: 0,
  }
}

async function load() {
  loading.value = true
  try {
    if (isUser.value) {
      const [me, byModel] = await Promise.all([portalApi.me(), portalApi.teamUsageByModel(days.value)])
      users.value = [selfToUser(me)]
      usage.value = byModel.filter((r) => r.user_id === me.id)
    } else {
      const [u, us] = await Promise.all([logsApi.usageByUserModel(days.value), usersApi.list()])
      usage.value = u
      users.value = us
    }
  } finally {
    loading.value = false
    await renderChart()
  }
}

function modelRows(r: Row): UserModelUsage[] {
  return [...r.models].sort((a, b) => b.total_tokens - a.total_tokens)
}

// 区间内按模型聚合（跨用户），用于 Top Models 图表
const modelAgg = computed(() => {
  const m = new Map<string, { tokens: number; cost: number; requests: number }>()
  for (const r of usage.value) {
    const c = m.get(r.model) || { tokens: 0, cost: 0, requests: 0 }
    c.tokens += r.total_tokens
    c.cost += r.cost
    c.requests += r.requests
    m.set(r.model, c)
  }
  return [...m.entries()].map(([model, v]) => ({ model, ...v })).sort((a, b) => b.tokens - a.tokens).slice(0, 12)
})

const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

async function renderChart() {
  await nextTick()
  if (mode.value !== 'range' || !chartEl.value || !modelAgg.value.length) {
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
        return `${p.name}<br/>tokens: ${humanNum(row.tokens)}<br/>requests: ${row.requests}<br/>cost: $${row.cost.toFixed(4)}`
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

onMounted(async () => {
  await load()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
})
</script>

<template>
  <div v-loading="loading">
    <el-card shadow="never" class="toolbar-card">
      <div class="toolbar">
        <el-radio-group v-model="mode" size="small">
          <el-radio-button value="lifetime">Lifetime</el-radio-button>
          <el-radio-button value="range">Range</el-radio-button>
        </el-radio-group>
        <el-radio-group v-if="mode === 'range'" v-model="days" size="small" @change="load">
          <el-radio-button :value="1">Today</el-radio-button>
          <el-radio-button :value="7">7 days</el-radio-button>
          <el-radio-button :value="30">30 days</el-radio-button>
          <el-radio-button :value="0">All</el-radio-button>
        </el-radio-group>
        <span class="hint">
          {{ mode === 'lifetime'
            ? 'Lifetime totals.'
            : 'Selected range.' }}
        </span>
      </div>
    </el-card>

    <el-row :gutter="14" class="mt">
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.requests) }}</div><div class="sum-label">Requests</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.prompt) }} / {{ humanNum(totals.completion) }}</div><div class="sum-label">Upload / Download tokens</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num" :class="{ neg: totals.callsBalance < 0 }">{{ totals.callsBalance }}</div><div class="sum-label">Calls balance{{ isUser ? '' : ' (all users)' }}</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num" :class="{ neg: totals.creditBalance < 0 }">${{ totals.creditBalance.toFixed(2) }}</div><div class="sum-label">Credit balance{{ isUser ? '' : ' (all users)' }}</div></el-card></el-col>
    </el-row>

    <el-card v-if="mode === 'range' && modelAgg.length" shadow="never" class="mt">
      <template #header>
        <div class="head"><span>Top Models</span><span class="hint">tokens in range</span></div>
      </template>
      <div ref="chartEl" style="height: 340px"></div>
    </el-card>

    <el-card shadow="never" class="mt">
      <el-table :data="rows" size="small" row-key="user_id">
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
                <el-table-column label="Cost ($)" width="110" align="right">
                  <template #default="{ row: m }">{{ money(m.cost) }}</template>
                </el-table-column>
              </el-table>
              <el-empty v-else description="No usage in this range" :image-size="40" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="User" min-width="170">
          <template #default="{ row }">{{ row.user_name || '—' }}</template>
        </el-table-column>
        <el-table-column label="Requests" width="100" align="right">
          <template #default="{ row }">{{ humanNum(mode === 'lifetime' ? row.calls_spent : row.requests) }}</template>
        </el-table-column>
        <el-table-column label="Upload" width="110" align="right">
          <template #default="{ row }">{{ humanNum(mode === 'lifetime' ? row.in_spent : row.prompt) }}</template>
        </el-table-column>
        <el-table-column label="Download" width="110" align="right">
          <template #default="{ row }">{{ humanNum(mode === 'lifetime' ? row.out_spent : row.completion) }}</template>
        </el-table-column>
        <el-table-column label="Cost ($)" width="100" align="right">
          <template #default="{ row }">{{ money(mode === 'lifetime' ? row.credit_spent : row.cost) }}</template>
        </el-table-column>
        <el-table-column label="Calls (bal)" width="110" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.calls_balance < 0 }">{{ row.calls_balance }}</span>
            <div class="sub">{{ row.calls_spent }} / {{ row.calls_earned }}</div>
          </template>
        </el-table-column>
        <el-table-column label="Credit (bal)" width="120" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.credit_balance < 0 }">${{ row.credit_balance.toFixed(4) }}</span>
            <div class="sub">${{ row.credit_spent.toFixed(4) }} / ${{ row.credit_earned.toFixed(4) }}</div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !rows.length" description="No users yet" />
    </el-card>
  </div>
</template>

<style scoped>
.toolbar-card { background: transparent; border: none; box-shadow: none; padding: 0; }
.toolbar { display: flex; align-items: center; gap: 12px; }
.hint { font-size: 12px; color: var(--ll-muted); }
.head { display: flex; justify-content: space-between; align-items: center; }
.sum { margin-bottom: 14px; }
.sum-num { font-size: 22px; font-weight: 700; color: var(--ll-ink); }
.sum-label { font-size: 12px; color: var(--ll-muted); margin-top: 2px; }
.neg { color: #c9808a; }
.sub { font-size: 11px; color: var(--ll-muted); }
.expand-wrap { padding: 8px 16px 12px; }
.mt { margin-top: 14px; }
</style>
