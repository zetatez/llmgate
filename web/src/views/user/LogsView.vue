<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { RequestLog } from '../../api'

const logs = ref<RequestLog[]>([])
const loading = ref(false)
const page = ref(1)
const size = ref(50)
const total = ref(0)

async function load() {
  loading.value = true
  try {
    const offset = (page.value - 1) * size.value
    logs.value = await portalApi.logs({ limit: size.value, offset })
    total.value = await portalApi.logsCount()
  } finally {
    loading.value = false
  }
}

function onSizeChange() {
  page.value = 1
  load()
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
        <span />
        <div>
          <el-select v-model="size" style="width: 120px" @change="onSizeChange">
            <el-option label="50 / page" :value="50" />
            <el-option label="100 / page" :value="100" />
            <el-option label="200 / page" :value="200" />
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
.head { display: flex; justify-content: space-between; align-items: center; }
.ml { margin-left: 8px; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
