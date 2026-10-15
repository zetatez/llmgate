<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { RequestLog } from '../../api'

const logs = ref<RequestLog[]>([])
const loading = ref(false)
const limit = ref(100)

async function load() {
  loading.value = true
  try { logs.value = await portalApi.logs(limit.value) } finally { loading.value = false }
}

function fmtTime(ts: number) {
  return ts ? new Date(ts * 1000).toLocaleString() : ''
}
function money(v: number) {
  return (v ?? 0).toFixed(4)
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="head">
        <span>My Requests</span>
        <div>
          <el-select v-model="limit" style="width: 120px" @change="load">
            <el-option label="Last 50" :value="50" />
            <el-option label="Last 100" :value="100" />
            <el-option label="Last 200" :value="200" />
            <el-option label="Last 500" :value="500" />
          </el-select>
          <el-button size="small" class="ml" :loading="loading" @click="load">Refresh</el-button>
        </div>
      </div>
    </template>

    <el-table v-loading="loading" :data="logs" size="small" max-height="620">
      <el-table-column label="Time" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="display_model" label="Model" min-width="150" show-overflow-tooltip />
      <el-table-column prop="channel_name" label="Channel" width="130" show-overflow-tooltip>
        <template #default="{ row }">{{ row.channel_name || '—' }}</template>
      </el-table-column>
      <el-table-column prop="prompt_tokens" label="In" width="90" sortable />
      <el-table-column prop="completion_tokens" label="Out" width="90" sortable />
      <el-table-column prop="total_tokens" label="Total" width="100" sortable />
      <el-table-column label="Cost ($)" width="100" sortable>
        <template #default="{ row }">{{ money(row.cost) }}</template>
      </el-table-column>
      <el-table-column prop="latency_ms" label="Latency" width="100" sortable>
        <template #default="{ row }">{{ row.latency_ms }}ms</template>
      </el-table-column>
      <el-table-column label="Status" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error_code" label="Error" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.error_code || '—' }}</template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && !logs.length" description="No requests yet" />
  </el-card>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.ml { margin-left: 8px; }
</style>
