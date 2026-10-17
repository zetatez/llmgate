<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import { logsApi, settingsApi } from '../api'
import type { DashboardPayload } from '../api'
import { ElMessage } from 'element-plus'
import { Connection, ChatDotRound, Money, WarningFilled, CopyDocument, Refresh } from '@element-plus/icons-vue'

const router = useRouter()
const payload = ref<DashboardPayload | null>(null)
const loading = ref(false)
const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
const hourEl = ref<HTMLDivElement>()
let hourChart: echarts.ECharts | null = null
const hourMode = ref<'cost' | 'tokens'>('cost')
const gatewayBase = ref('')
const days = ref(7)

function humanNum(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '-'
  const abs = Math.abs(n)
  if (abs >= 1e9) return +(n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return +(n / 1e3).toFixed(1) + 'k'
  return String(n)
}

function humanMoney(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '-'
  if (Math.abs(n) >= 1000) return (n / 1000).toFixed(2) + 'k'
  return n.toFixed(4)
}

async function load() {
  loading.value = true
  try {
    payload.value = await logsApi.dashboard(days.value)
  } catch (e: any) {
    ElMessage.error('Failed to load overview: ' + (e?.message || e))
  } finally {
    loading.value = false
    // 等待 v-if 的图表容器渲染完成后再初始化，否则首屏 ref 为空导致图表不显示
    await nextTick()
    renderChart()
    renderHourChart()
  }
}

async function loadGatewayBase() {
  try {
    const items = await settingsApi.get()
    const p = items.find((i) => i.key === 'gateway_prefix')?.val || '/'
    const prefix = p === '/' ? '' : p.replace(/\/$/, '')
    gatewayBase.value = `${window.location.origin}${prefix}/v1`
  } catch { /* keep fallback */ }
}

function copyGatewayBase() {
  navigator.clipboard?.writeText(gatewayBase.value)
  ElMessage.success('Copied')
}

// 区间统计（按所选天数求和）
const range = computed(() => {
  const w = payload.value?.week || []
  return w.reduce((s, p) => ({
    req: s.req + (p.requests || 0),
    tokens: s.tokens + (p.total_tokens || 0),
    cost: s.cost + (p.cost || 0),
    errors: s.errors + (p.errors || 0),
  }), { req: 0, tokens: 0, cost: 0, errors: 0 })
})

interface Stat {
  label: string
  value: string
  today: string
  icon: unknown
  color: string
  bg: string
}

const stats = computed<Stat[]>(() => {
  const r = range.value
  const t = payload.value?.today
  return [
    { label: 'Requests', value: humanNum(r.req), today: `today ${t?.total_requests ?? 0}`, icon: Connection, color: 'var(--el-color-primary)', bg: 'rgba(124,134,214,.15)' },
    { label: 'Tokens', value: humanNum(r.tokens), today: `today ${humanNum(t?.total_tokens)}`, icon: ChatDotRound, color: '#6f9a86', bg: 'rgba(111,154,134,.15)' },
    { label: 'Spend ($)', value: humanMoney(r.cost), today: `today ${humanMoney(t?.cost)}`, icon: Money, color: '#b18a4f', bg: 'rgba(177,138,79,.16)' },
    { label: 'Errors', value: humanNum(r.errors), today: `today ${t?.errors ?? 0}`, icon: WarningFilled, color: r.errors > 0 ? '#c9808a' : '#6f9a86', bg: r.errors > 0 ? 'rgba(201,128,138,.16)' : 'rgba(111,154,134,.15)' },
  ]
})

const weekEmpty = computed(() => {
  const w = payload.value?.week || []
  return w.length === 0 || w.every((p) => p.total_tokens === 0 && p.requests === 0)
})



function renderHourChart() {
  if (!hourEl.value) return
  const hours = payload.value?.hours || []
  hourChart?.dispose()
  hourChart = echarts.init(hourEl.value)
  const mode = hourMode.value
  hourChart.setOption({
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(18,22,32,.92)',
      borderWidth: 0,
      textStyle: { color: '#fff' },
      valueFormatter: (v: any) => (mode === 'cost' ? '$' + (v || 0).toFixed(4) : humanNum(v) + ' token'),
    },
    grid: { left: 56, right: 16, top: 16, bottom: 26 },
    xAxis: { type: 'category', data: hours.map((h) => h.hour), axisTick: { show: false }, axisLine: { lineStyle: { color: '#2a2c33' } }, axisLabel: { color: '#8b909c', interval: 2 } },
    yAxis: { type: 'value', axisLabel: { formatter: (v: number) => (mode === 'cost' ? '$' + humanMoney(v) : humanNum(v)), color: '#8b909c' }, splitLine: { lineStyle: { color: '#23252c' } } },
    series: [{
      type: 'bar',
      barMaxWidth: 14,
      data: hours.map((h) => (mode === 'cost' ? h.cost : h.tokens)),
      itemStyle: { color: '#8b93d6', borderRadius: [3, 3, 0, 0] },
    }],
  })
  hourChart.resize()
}

watch(hourMode, () => renderHourChart())

// Top 模型（按区间 token 汇总 + 占比条）
const topModels = computed(() => {
  const marks = payload.value?.week_models || []
  const totals = new Map<string, number>()
  for (const m of marks) totals.set(m.model, (totals.get(m.model) || 0) + m.tokens)
  const arr = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10)
  const sum = arr.reduce((s, [, v]) => s + v, 0) || 1
  return arr.map(([model, tokens]) => ({ model, tokens, share: (tokens / sum) * 100 }))
})

const MODEL_COLORS = ['#8b93d6', '#8fae9b', '#d3b27f', '#c98b96', '#7a93b8', '#b5a7dd', '#a3b0c2', '#d9a05f', '#9aa7c9', '#c9c2dc']

function renderChart() {
  if (!payload.value || !chartEl.value) return
  const daysLabel = payload.value.week.map((p) => p.date)
  const marks = payload.value.week_models || []
  const totals = new Map<string, number>()
  for (const m of marks) totals.set(m.model, (totals.get(m.model) || 0) + m.tokens)
  const topModelsArr = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 7).map(([m]) => m)

  const seriesByModel = new Map<string, number[]>()
  for (const m of marks) {
    if (!topModelsArr.includes(m.model)) continue
    if (!seriesByModel.has(m.model)) seriesByModel.set(m.model, daysLabel.map(() => 0))
    const idx = daysLabel.indexOf(m.date)
    if (idx >= 0) seriesByModel.get(m.model)![idx] += m.tokens
  }
  const costs = daysLabel.map((d) => payload.value!.week.find((x) => x.date === d)?.cost || 0)

  chart?.dispose()
  chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: 'rgba(18,22,32,.92)',
      borderWidth: 0,
      textStyle: { color: '#fff' },
      formatter(params: any[]) {
        const p0 = params[0]
        let html = p0.axisValue + '<br/>'
        const token = params.filter((p: any) => p.seriesName !== 'Spend ($)')
        const total = token.reduce((s: number, p: any) => s + (p.value || 0), 0)
        for (const p of token) {
          const share = total > 0 ? ' (' + ((p.value / total) * 100).toFixed(1) + '%)' : ''
          html += `${p.marker}${p.seriesName}: ${humanNum(p.value)} token${share}<br/>`
        }
        const sp = params.find((p: any) => p.seriesName === 'Spend ($)')
        if (sp) html += `${sp.marker}Spend: $${(sp.value || 0).toFixed(4)}<br/>`
        html += `<b>Total: ${humanNum(total)} token</b>`
        return html
      },
    },
    legend: { type: 'scroll', top: 0, left: 0, right: 0, icon: 'roundRect', itemWidth: 10, itemHeight: 10, itemGap: 12, textStyle: { color: '#8b909c', fontSize: 11 } },
    grid: { left: 48, right: 48, top: 40, bottom: 26 },
    xAxis: { type: 'category', data: daysLabel, axisTick: { show: false }, axisLine: { lineStyle: { color: '#2a2c33' } }, axisLabel: { color: '#8b909c' } },
    yAxis: [
      { type: 'value', axisLabel: { formatter: (v: number) => humanNum(v), color: '#8b909c' }, splitLine: { lineStyle: { color: '#23252c' } } },
      { type: 'value', axisLabel: { formatter: (v: number) => '$' + humanMoney(v), color: '#b18a4f' }, splitLine: { show: false } },
    ],
    series: [
      ...topModelsArr.map((m, i) => ({
        name: m,
        type: 'bar',
        stack: 'models',
        barMaxWidth: 26,
        data: daysLabel.map((_, j) => seriesByModel.get(m)![j] || 0),
        itemStyle: { color: MODEL_COLORS[i % MODEL_COLORS.length], borderRadius: i === topModelsArr.length - 1 ? [4, 4, 0, 0] : [0, 0, 0, 0] },
      })),
      {
        name: 'Spend ($)',
        type: 'line',
        yAxisIndex: 1,
        smooth: true,
        symbolSize: 5,
        data: costs,
        lineStyle: { width: 2, color: '#b18a4f' },
        itemStyle: { color: '#b18a4f' },
      },
    ],
  })
}

function onResize() {
  chart?.resize()
  hourChart?.resize()
}

onMounted(async () => {
  await Promise.all([load(), loadGatewayBase()])
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
  hourChart?.dispose()
})
</script>

<template>
  <div v-loading="loading">
    <!-- 顶行：时间范围 + 刷新 -->
    <el-card shadow="never" class="toolbar-card">
      <div class="toolbar">
        <el-radio-group v-model="days" size="small" @change="load">
          <el-radio-button :value="1">Today</el-radio-button>
          <el-radio-button :value="7">7 days</el-radio-button>
          <el-radio-button :value="30">30 days</el-radio-button>
        </el-radio-group>
        <el-button size="small" text :loading="loading" @click="load"><el-icon :size="15"><Refresh /></el-icon>&nbsp;Refresh</el-button>
        <div class="spacer" />
        <div class="gateway">
          <span class="gl">Gateway</span>
          <code>{{ gatewayBase }}</code>
          <el-button link type="primary" size="small" @click="copyGatewayBase"><el-icon :size="14"><CopyDocument /></el-icon>&nbsp;copy</el-button>
        </div>
      </div>
    </el-card>

    <!-- 统计卡（区间） -->
    <el-row :gutter="14" class="mt">
      <el-col v-for="s in stats" :key="s.label" :xs="12" :md="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-row">
            <div class="stat-icon" :style="{ background: s.bg, color: s.color }">
              <el-icon :size="20"><component :is="s.icon" /></el-icon>
            </div>
            <div class="stat-meta">
              <div class="stat-num">{{ s.value }}</div>
              <div class="stat-label">{{ s.label }} · <span class="stat-today">{{ s.today }}</span></div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 图表并排：按小时 + 用量趋势 -->
    <el-row :gutter="14" class="mt">
      <el-col :xs="24" :lg="10">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="head">
              <span>Hourly (last 24h)</span>
              <el-radio-group v-model="hourMode" size="small">
                <el-radio-button value="cost">Cost</el-radio-button>
                <el-radio-button value="tokens">Tokens</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <div ref="hourEl" style="height: 208px"></div>
        </el-card>
      </el-col>
      <el-col :xs="24" :lg="14">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="head">
              <span>Usage trend</span>
              <span class="week-sum">{{ humanNum(range.tokens) }} tokens · ${{ range.cost.toFixed(2) }}</span>
            </div>
          </template>
          <template v-if="!weekEmpty">
            <div ref="chartEl" style="height: 208px"></div>
          </template>
          <el-empty v-else description="No usage in the selected period" :image-size="60" />
        </el-card>
      </el-col>
    </el-row>

    <!-- 底部三栏：Top 模型 / 渠道 / 失败 -->
    <el-row :gutter="14" class="mt">
      <el-col :xs="24" :md="8">
        <el-card shadow="never">
          <template #header>Top Models</template>
          <template v-if="topModels.length">
            <div v-for="m in topModels" :key="m.model" class="model-item">
              <div class="model-head">
                <span class="model-name" :title="m.model">{{ m.model }}</span>
                <span class="model-val">{{ humanNum(m.tokens) }} <span class="dim">tok</span></span>
              </div>
              <div class="bar"><div class="bar-inner" :style="{ width: m.share + '%' }"></div></div>
            </div>
          </template>
          <el-empty v-else description="No model usage yet" :image-size="50" />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="8">
        <el-card shadow="never">
          <template #header>
            <div class="head"><span>Channels</span><el-button link type="primary" size="small" @click="router.push('/channels')">Manage</el-button></div>
          </template>
          <el-table :data="payload?.channel_stats ?? []" size="small">
            <el-table-column prop="name" label="Channel" />
            <el-table-column prop="health" label="Status" width="105">
              <template #default="{ row }">
                <el-tag :type="row.health === 'cooldown' ? 'danger' : 'success'" size="small" effect="light">{{ row.health === 'cooldown' ? 'Cooldown' : 'Healthy' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="today_requests" label="Req" align="right" />
            <el-table-column label="Err" align="right">
              <template #default="{ row }"><span :class="row.today_errors > 0 ? 'err' : ''">{{ row.today_errors }}</span></template>
            </el-table-column>
          </el-table>
          <el-alert v-if="!payload?.channel_stats?.length" class="mt" title="No channels configured" type="info" :closable="false" />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="8">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Recent Failures</span>
              <div>
                <el-tag v-if="payload?.recent_failures" type="danger" size="small">{{ payload.recent_failures }}</el-tag>
                <el-button link type="primary" size="small" class="ml" @click="router.push('/logs')">View logs</el-button>
              </div>
            </div>
          </template>
          <el-table :data="payload?.failures ?? []" size="small" max-height="248">
            <el-table-column label="Time" width="96">
              <template #default="{ row }">{{ new Date(row.created_at * 1000).toLocaleString().slice(5, 16) }}</template>
            </el-table-column>
            <el-table-column prop="display_model" label="Model" min-width="100" show-overflow-tooltip />
            <el-table-column prop="error_code" label="Error" min-width="100" show-overflow-tooltip>
              <template #default="{ row }"><span class="dim err">{{ row.error_code || row.status }}</span></template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!payload?.failures?.length" description="No failures lately" :image-size="50" />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.chart-card { height: 100%; }
.chart-card :deep(.el-card__body) { padding: 8px 12px 4px; }
.toolbar-card {
  background: transparent;
  border: none;
  box-shadow: none;
  padding: 0;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
}
.spacer {
  flex: 1;
}
.gateway {
  display: flex;
  align-items: center;
  gap: 6px;
}
.gateway .gl {
  font-size: 12px;
  color: var(--ll-muted);
}
.gateway code {
  background: var(--ll-soft);
  border-radius: 6px;
  padding: 2px 10px;
  color: var(--el-color-primary);
  font-size: 13px;
}
.stat-card {
  margin-bottom: 12px;
}
.stat-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.stat-icon {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-meta {
  min-width: 0;
}
.stat-num {
  font-size: 20px;
  font-weight: 700;
  color: var(--ll-ink);
  line-height: 1.2;
  white-space: nowrap;
}
.stat-label {
  color: var(--ll-muted);
  font-size: 13px;
  margin-top: 2px;
  white-space: nowrap;
}
.stat-today {
  color: var(--ll-muted);
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.week-sum {
  font-size: 12px;
  color: var(--ll-muted);
}
.model-item {
  margin-bottom: 12px;
}
.model-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
}
.model-name {
  color: var(--ll-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.model-val {
  color: var(--ll-muted);
  white-space: nowrap;
}
.bar {
  height: 6px;
  border-radius: 3px;
  background: var(--ll-soft);
  margin-top: 5px;
  overflow: hidden;
}
.bar-inner {
  height: 100%;
  border-radius: 3px;
  background: linear-gradient(90deg, #8b93d6, #b5a7dd);
}
.mt {
  margin-top: 12px;
}
.ml {
  margin-left: 6px;
}
.err {
  color: #c9808a;
  font-weight: 600;
}
.dim {
  font-weight: normal;
  font-size: 12px;
}
</style>