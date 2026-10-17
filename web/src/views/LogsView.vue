<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { logsApi } from '../api'
import type { RequestLog } from '../api'

const list = ref<RequestLog[]>([])
const loading = ref(false)
const status = ref('')
const displayModel = ref('')
const page = ref(1)
const size = ref(50)
const total = ref(0)

function fmtTime(ts: number) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function baseParams(): Record<string, string | number> {
  const params: Record<string, string | number> = {}
  if (status.value) params.status = status.value
  if (displayModel.value) params.display_model = displayModel.value
  return params
}

async function search() {
  page.value = 1
  await load()
}

async function load() {
  loading.value = true
  try {
    const params = { ...baseParams(), limit: size.value, offset: (page.value - 1) * size.value }
    list.value = await logsApi.list(params)
    total.value = await logsApi.count(baseParams())
  } finally {
    loading.value = false
  }
}

function onSizeChange() {
  page.value = 1
  load()
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="filters">
        <el-input v-model="displayModel" placeholder="Model" clearable style="width: 160px" @keyup.enter="search" />
        <el-select v-model="status" placeholder="Status" clearable style="width: 110px">
          <el-option label="Success" value="success" />
          <el-option label="Failed" value="error" />
        </el-select>
        <el-select v-model="size" style="width: 120px" @change="onSizeChange">
          <el-option label="50 / page" :value="50" />
          <el-option label="100 / page" :value="100" />
          <el-option label="200 / page" :value="200" />
        </el-select>
        <el-button type="primary" @click="search">Search</el-button>
      </div>
    </template>
    <el-table v-loading="loading" :data="list" size="small" max-height="70vh">
      <el-table-column label="Time" width="165" class-name="nowrap">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="display_model" label="Model" width="170" show-overflow-tooltip />
      <el-table-column label="User" min-width="130" show-overflow-tooltip>
        <template #default="{ row }">{{ row.user_name || '—' }}</template>
      </el-table-column>
      <el-table-column label="Channel" width="130" show-overflow-tooltip>
        <template #default="{ row }">{{ row.channel_name || `#${row.channel_id}` }}</template>
      </el-table-column>
      <el-table-column label="Upstream" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">
          {{ row.channel_name ? `${row.channel_name}/${row.upstream_model}` : row.upstream_model }}
        </template>
      </el-table-column>
      <el-table-column label="Tokens" width="90" class-name="nowrap">
        <template #default="{ row }">{{ row.total_tokens }}</template>
      </el-table-column>
      <el-table-column prop="latency_ms" label="Latency (ms)" width="100" class-name="nowrap" />
      <el-table-column label="Status" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error_code" label="Error" min-width="170" show-overflow-tooltip />
    </el-table>

    <div class="pager">
      <el-pagination
        v-model:current-page="page"
        :page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        :disabled="loading"
        @current-change="load"
      />
    </div>
  </el-card>
</template>

<style scoped>
.filters { display: flex; gap: 8px; align-items: center; }
.nowrap { white-space: nowrap; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
