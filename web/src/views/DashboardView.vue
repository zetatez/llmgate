<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import { logsApi, settingsApi } from '../api'
import type { DashboardPayload } from '../api'
import { ElMessage } from 'element-plus'
import { Connection, ChatDotRound, Money, WarningFilled, CopyDocument, Plus, User } from '@element-plus/icons-vue'

const router = useRouter()
const payload = ref<DashboardPayload | null>(null)
const loading = ref(false)
const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
const gatewayBase = ref('')

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
    payload.value = await logsApi.dashboard()
  } catch (e: any) {
    ElMessage.error('Failed to load dashboard: ' + (e?.message || e))
  } finally {
    loading.value = false
    renderChart()
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

interface Stat {
  label: string
  value: string
  title: string
  icon: unknown
  color: string
  bg: string
}

const stats = computed<Stat[]>(() => {
  const t = payload.value?.today
  return [
    { label: 'Requests Today', value: humanNum(t?.total_requests), title: `${t?.total_requests ?? 0} requests`, icon: Connection, color: '#7c86d6', bg: '#eef0f9' },
    { label: 'Tokens Today', value: humanNum(t?.total_tokens), title: `${t?.total_tokens ?? 0} tokens`, icon: ChatDotRound, color: '#6f9a86', bg: '#e9f4ee' },
    { label: 'Cost Today ($)', value: humanMoney(t?.cost), title: `$${(t?.cost ?? 0).toFixed(4)}`, icon: Money, color: '#b18a4f', bg: '#faf3e6' },
    { label: 'Errors Today', value: humanNum(t?.errors), title: `${t?.errors ?? 0} errors`, icon: WarningFilled, color: (t?.errors || 0) > 0 ? '#c9808a' : '#6f9a86', bg: (t?.errors || 0) > 0 ? '#fbeff0' : '#e9f6ee' },
  ]
})

const weekEmpty = computed(() => {
  const w = payload.value?.week || []
  return w.length === 0 || w.every((p) => p.total_tokens === 0 && p.requests === 0)
})

// 近 7 天总览（次级信息行）
const weekTotal = computed(() => {
  const w = payload.value?.week || []
  return w.reduce((s, p) => ({ tokens: s.tokens + (p.total_tokens || 0), cost: s.cost + (p.cost || 0), req: s.req + (p.requests || 0) }), { tokens: 0, cost: 0, req: 0 })
})

const MODEL_COLORS = ['#8b93d6', '#8fae9b', '#d3b27f', '#c98b96', '#7a93b8', '#b5a7dd', '#a3b0c2', '#d9a05f', '#9aa7c9', '#c9c2dc']

function renderChart() {
  if (!payload.value || !chartEl.value) return
  const days = payload.value.week.map((p) => p.date)
  const marks = payload.value.week_models || []
  const totals = new Map<string, number>()
  for (const m of marks) totals.set(m.model, (totals.get(m.model) || 0) + m.tokens)
  const topModels = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 7).map(([m]) => m)

  const seriesByModel = new Map<string, number[]>()
  for (const m of marks) {
    if (!topModels.includes(m.model)) continue
    if (!seriesByModel.has(m.model)) seriesByModel.set(m.model, days.map(() => 0))
    const idx = days.indexOf(m.date)
    if (idx >= 0) seriesByModel.get(m.model)![idx] += m.tokens
  }

  chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: 'rgba(18,22,32,.92)',
      borderWidth: 0,
      textStyle: { color: '#fff' },
      formatter(params: any[]) {
        const total = params.reduce((s: number, p: any) => s + (p.value || 0), 0)
        let html = params[0].axisValue + '<br/>'
        for (const p of params) {
          const share = total > 0 ? ' (' + ((p.value / total) * 100).toFixed(1) + '%)' : ''
          html += `${p.marker}${p.seriesName}: ${humanNum(p.value)} token${share}<br/>`
        }
        html += `<b>Total: ${humanNum(total)} token</b>`
        return html
      },
    },
    legend: { type: 'scroll', bottom: 0, icon: 'roundRect', itemWidth: 10, itemHeight: 10 },
    grid: { left: 48, right: 16, top: 24, bottom: 34 },
    xAxis: { type: 'category', data: days, axisTick: { show: false }, axisLine: { lineStyle: { color: '#e5e7eb' } }, axisLabel: { color: '#6b7280' } },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: (v: number) => humanNum(v), color: '#6b7280' },
      splitLine: { lineStyle: { color: '#f0f1f5' } },
    },
    series: topModels.map((m, i) => ({
      name: m,
      type: 'bar',
      stack: 'models',
      barMaxWidth: 26,
      data: days.map((_, j) => seriesByModel.get(m)![j] || 0),
      itemStyle: { color: MODEL_COLORS[i % MODEL_COLORS.length], borderRadius: i === topModels.length - 1 ? [4, 4, 0, 0] : [0, 0, 0, 0] },
    })),
  })
}

function onResize() {
  chart?.resize()
}

onMounted(async () => {
  await Promise.all([load(), loadGatewayBase()])
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
})
</script>

<template>
  <div v-loading="loading">
    <!-- 统计卡片 -->
    <el-row :gutter="14">
      <el-col v-for="s in stats" :key="s.label" :xs="12" :sm="12" :md="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-row">
            <div class="stat-icon" :style="{ background: s.bg, color: s.color }">
              <el-icon :size="20"><component :is="s.icon" /></el-icon>
            </div>
            <div class="stat-meta">
              <div class="stat-num" :title="s.title">{{ s.value }}</div>
              <div class="stat-label">{{ s.label }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 快捷操作：网关接入 -->
    <el-card shadow="never" class="mt">
      <div class="quick">
        <div class="quick-gateway">
          <div class="quick-label">Gateway Base URL</div>
          <div class="quick-value">
            <code>{{ gatewayBase }}</code>
            <el-button link type="primary" size="small" @click="copyGatewayBase"><el-icon :size="14"><CopyDocument /></el-icon>&nbsp;Copy</el-button>
          </div>
        </div>
        <el-button type="primary" plain @click="router.push('/channels')"><el-icon class="btn-icon"><Plus /></el-icon>Add Channel</el-button>
        <el-button plain @click="router.push('/users')"><el-icon class="btn-icon"><User /></el-icon>Users</el-button>
      </div>
    </el-card>

    <!-- 用量趋势 -->
    <el-card shadow="never" class="mt">
      <template #header>
        <div class="head">
          <span>Usage Trend (Last 7 Days)</span>
          <span class="week-sum">tokens {{ humanNum(weekTotal.tokens) }} · ${{ (weekTotal.cost).toFixed(2) }} · {{ humanNum(weekTotal.req) }} req</span>
        </div>
      </template>
      <template v-if="!weekEmpty">
        <div ref="chartEl" style="height: 300px"></div>
      </template>
      <el-empty v-else description="No usage in the last 7 days yet" :image-size="70" />
    </el-card>

    <!-- 渠道 / 失败 -->
    <el-row :gutter="14" class="mt">
      <el-col :xs="24" :md="14">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Channels Today</span>
              <el-button link type="primary" size="small" @click="router.push('/channels')">Manage</el-button>
            </div>
          </template>
          <el-table :data="payload?.channel_stats ?? []" size="small">
            <el-table-column prop="name" label="Channel" />
            <el-table-column prop="health" label="Status" width="110">
              <template #default="{ row }">
                <el-tag :type="row.health === 'cooldown' ? 'danger' : 'success'" size="small" effect="light">
                  {{ row.health === 'cooldown' ? 'Cooldown' : 'Healthy' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="today_requests" label="Requests" align="right" />
            <el-table-column label="Errors" align="right">
              <template #default="{ row }">
                <span :class="row.today_errors > 0 ? 'err' : ''">{{ row.today_errors }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-alert v-if="!payload?.channel_stats?.length" class="mt" title="No channels configured yet" type="info" :closable="false" />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="10">
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
          <el-table :data="payload?.failures ?? []" size="small" max-height="288">
            <el-table-column label="Time" width="100">
              <template #default="{ row }">{{ new Date(row.created_at * 1000).toLocaleString().slice(5, 16) }}</template>
            </el-table-column>
            <el-table-column prop="display_model" label="Model" min-width="110" show-overflow-tooltip />
            <el-table-column prop="channel_id" label="Ch" width="45" align="center" />
            <el-table-column prop="error_code" label="Error" min-width="110" show-overflow-tooltip>
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
.stat-card {
  margin-bottom: 14px;
}
.stat-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 4px 2px;
}
.stat-icon {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-meta {
  min-width: 0;
}
.stat-num {
  font-size: 24px;
  font-weight: 700;
  color: #1f2430;
  line-height: 1.2;
  white-space: nowrap;
}
.stat-label {
  color: #8a90a0;
  font-size: 13px;
  margin-top: 2px;
  white-space: nowrap;
}
.quick {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.quick-gateway {
  margin-right: auto;
}
.quick-label {
  font-size: 12px;
  color: #8a90a0;
}
.quick-value {
  display: flex;
  align-items: center;
  gap: 6px;
}
.quick-value code {
  background: #f1f2f7;
  border-radius: 6px;
  padding: 2px 10px;
  color: #7c86d6;
  font-size: 13px;
}
.btn-icon {
  margin-right: 2px;
}
.mt {
  margin-top: 14px;
}
.ml {
  margin-left: 6px;
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
  color: #8a90a0;
}
.err {
  color: #e11d48;
  font-weight: 600;
}
.dim {
  font-weight: normal;
  font-size: 12px;
}
</style>