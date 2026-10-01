<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { logsApi } from '../api'
import type { RequestLog } from '../api'

const list = ref<RequestLog[]>([])
const loading = ref(false)
const status = ref('')
const displayModel = ref('')
const limit = ref(50)

function fmtTime(ts: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

async function load() {
  loading.value = true
  try {
    const params: Record<string, string | number> = { limit: limit.value }
    if (status.value) params.status = status.value
    if (displayModel.value) params.display_model = displayModel.value
    list.value = await logsApi.list(params)
  } finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="filters">
        <el-input v-model="displayModel" placeholder="Model" clearable style="width: 160px" @keyup.enter="load" />
        <el-select v-model="status" placeholder="Status" clearable style="width: 110px">
          <el-option label="Success" value="success" />
          <el-option label="Failed" value="error" />
        </el-select>
        <el-select v-model="limit" style="width: 100px">
          <el-option label="50" :value="50" />
          <el-option label="100" :value="100" />
          <el-option label="200" :value="200" />
        </el-select>
        <el-button type="primary" @click="load">Search</el-button>
      </div>
    </template>
    <el-table v-loading="loading" :data="list" size="small" max-height="70vh">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="Time" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="display_model" label="Model" width="140" />
      <el-table-column prop="channel_id" label="Ch." width="60" />
      <el-table-column prop="upstream_model" label="Upstream Model" width="130" show-overflow-tooltip />
      <el-table-column label="Tokens (p/c/t)" width="120">
        <template #default="{ row }">{{ row.prompt_tokens }} / {{ row.completion_tokens }} / {{ row.total_tokens }}</template>
      </el-table-column>
      <el-table-column prop="cost" label="Cost ($)" width="90" />
      <el-table-column prop="latency_ms" label="Latency (ms)" width="100" />
      <el-table-column label="Status" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error_code" label="Error" min-width="140" show-overflow-tooltip />
    </el-table>
  </el-card>
</template>

<style scoped>
.filters { display: flex; gap: 8px; align-items: center; }
</style>