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

function renderChart() {
  if (!payload.value || !chartEl.value) return
  const week = payload.value.week
  chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['Requests', 'Tokens (k)'] },
    grid: { left: 40, right: 40, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: week.map((p) => p.date) },
    yAxis: [
      { type: 'value', name: 'Requests' },
      { type: 'value', name: 'Tokens' },
    ],
    series: [
      { name: 'Requests', type: 'bar', data: week.map((p) => p.requests), itemStyle: { color: '#1677ff' } },
      { name: 'Tokens (k)', type: 'line', yAxisIndex: 1, smooth: true, data: week.map((p) => p.total_tokens / 1000), itemStyle: { color: '#13c2c2' } },
    ],
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
        <el-card shadow="never"><div class="stat"><div class="num">{{ payload?.today.total_requests ?? '-' }}</div><div class="label">Requests Today</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="num">{{ payload?.today.total_tokens ?? '-' }}</div><div class="label">Tokens Today</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="num">{{ payload?.today.cost?.toFixed(4) ?? '-' }}</div><div class="label">Cost Today ($)</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="num" :style="{ color: (payload?.today.errors || 0) > 0 ? '#f56c6c' : '#67c23a' }">{{ payload?.today.errors ?? '-' }}</div><div class="label">Errors Today</div></div></el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header>Usage Trend (Last 7 Days)</template>
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