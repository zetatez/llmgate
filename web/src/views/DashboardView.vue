<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import * as echarts from 'echarts'
import { logsApi } from '../api'
import type { DashboardPayload } from '../api'
import { ElMessage } from 'element-plus'

const payload = ref<DashboardPayload | null>(null)
const loading = ref(false)
const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

// 把大数字变成人可读的 k / M，方便一眼看懂（悬浮显示精确值）。
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
  }
}

// 仪表盘主色盘（模型用）。
const MODEL_COLORS = ['#5470c6', '#91cc75', '#fac858', '#ee6666', '#73c0de', '#3ba272', '#fc8452', '#9a60b4', '#ea7ccc']

// 把每日×模型数据折叠成「每天各模型 token 占比」的堆叠柱状图。
function renderChart() {
  if (!payload.value || !chartEl.value) return
  const days = payload.value.week.map((p) => p.date)
  const marks = payload.value.week_models || []

  // 各模型 7 天内累计 token，取 Top 7，其余并入"其他"，避免图例爆炸
  const totals = new Map<string, number>()
  for (const m of marks) totals.set(m.model, (totals.get(m.model) || 0) + m.tokens)
  const topModels = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 7).map(([m]) => m)

  // 组装每个模型的逐日 token（缺失补 0）；以原始 token 数量堆叠，
  // y 轴体现真实用量（不同日期的高低差异直观可见），而非归一化百分比。
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
      formatter(params: any[]) {
        const total = params.reduce((s: number, p: any) => s + (p.value || 0), 0)
        let html = params[0].axisValue + '<br/>'
        for (const p of params) {
          const share = total > 0 ? ' (' + ((p.value / total) * 100).toFixed(1) + '%)' : ''
          html += `${p.marker}${p.seriesName}: ${humanNum(p.value)} token${share}<br/>`
        }
        html += `合计: ${humanNum(total)} token`
        return html
      },
    },
    legend: { type: 'scroll', bottom: 0 },
    grid: { left: 60, right: 20, top: 30, bottom: 40 },
    xAxis: { type: 'category', data: days },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: (v: number) => humanNum(v) },
    },
    series: topModels.map((m, i) => ({
      name: m,
      type: 'bar',
      stack: 'models',
      data: days.map((_, j) => seriesByModel.get(m)![j] || 0),
      itemStyle: { color: MODEL_COLORS[i % MODEL_COLORS.length] },
    })),
  })
}

function onResize() {
  chart?.resize()
}

onMounted(async () => {
  await load()
  renderChart()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
})
</script>

<template>
  <div v-loading="loading">
    <el-row :gutter="12">
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat">
            <div class="num" :title="`${payload?.today.total_requests ?? 0} req`">{{ humanNum(payload?.today.total_requests) }}</div>
            <div class="label">Requests Today</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat">
            <div class="num" :title="`${payload?.today.total_tokens ?? 0} tokens`">{{ humanNum(payload?.today.total_tokens) }}</div>
            <div class="label">Tokens Today</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat">
            <div class="num" :title="`$${(payload?.today.cost ?? 0).toFixed(4)}`">{{ humanMoney(payload?.today.cost) }}</div>
            <div class="label">Cost Today ($)</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat">
            <div class="num" :style="{ color: (payload?.today.errors || 0) > 0 ? '#f56c6c' : '#67c23a' }" :title="`${payload?.today.errors ?? 0}`">{{ humanNum(payload?.today.errors) }}</div>
            <div class="label">Errors Today</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header>Usage Trend (Last 7 Days) · 分模型 Token 用量</template>
      <div ref="chartEl" style="height: 300px"></div>
    </el-card>

    <el-row :gutter="12" class="mt">
      <el-col :span="14">
        <el-card shadow="never">
          <template #header>Channels Today</template>
          <el-table :data="payload?.channel_stats ?? []" size="small">
            <el-table-column prop="name" label="Channel" />
            <el-table-column prop="health" label="Status">
              <template #default="{ row }">
                <el-tag :type="row.health === 'cooldown' ? 'danger' : 'success'" size="small">
                  {{ row.health === 'cooldown' ? 'Cooldown' : 'Healthy' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="today_requests" label="Requests" />
            <el-table-column prop="today_errors" label="Errors">
              <template #default="{ row }">
                <span :style="{ color: row.today_errors > 0 ? '#f56c6c' : '' }">{{ row.today_errors }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-alert v-if="!payload?.channel_stats?.length" class="mt" title="No channels configured yet" type="info" :closable="false" />
        </el-card>
      </el-col>
      <el-col :span="10">
        <el-card shadow="never">
          <template #header>
            <div class="head">
              <span>Recent Failures</span>
              <el-tag v-if="payload?.recent_failures" type="danger" size="small">{{ payload.recent_failures }}</el-tag>
            </div>
          </template>
          <el-table :data="payload?.failures ?? []" size="small" max-height="300">
            <el-table-column label="Time" width="100">
              <template #default="{ row }">{{ new Date(row.created_at * 1000).toLocaleString().slice(5, 16) }}</template>
            </el-table-column>
            <el-table-column prop="display_model" label="Model" min-width="110" show-overflow-tooltip />
            <el-table-column prop="channel_id" label="Ch" width="45" />
            <el-table-column prop="error_code" label="Error" min-width="110" show-overflow-tooltip>
              <template #default="{ row }">
                <span style="color:#f56c6c" class="dim">{{ row.error_code || row.status }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!payload?.failures?.length" description="No failures lately" :image-size="50" />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.stat { text-align: center; padding: 8px 0; }
.num { font-size: 26px; font-weight: 600; color: #303133; }
.label { color: #909399; font-size: 13px; margin-top: 4px; }
.mt { margin-top: 12px; }
.head { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px; }
</style>