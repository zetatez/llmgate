<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { PortalUser, UserOverview } from '../../api'

const me = ref<PortalUser | null>(null)
const overview = ref<UserOverview | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    me.value = await portalApi.me()
    overview.value = await portalApi.overview()
  } finally {
    loading.value = false
  }
}

function money(v?: number) {
  return (v ?? 0).toFixed(4)
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <el-row :gutter="12">
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">Account</div>
          <div class="stat-value">{{ me?.username || '—' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">Quota used ($)</div>
          <div class="stat-value">
            {{ money(me?.quota_used) }}
            <span class="dim">/ {{ me?.quota_limit ? money(me.quota_limit) : 'unlimited' }}</span>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">Today requests</div>
          <div class="stat-value">{{ overview?.today.total_requests ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">Today cost ($)</div>
          <div class="stat-value">{{ money(overview?.today.cost) }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header><span>Last 7 days</span></template>
      <el-table :data="overview?.week ?? []" size="small">
        <el-table-column prop="date" label="Date" width="140" />
        <el-table-column prop="requests" label="Requests" width="110" sortable />
        <el-table-column prop="success" label="Success" width="110" sortable />
        <el-table-column prop="errors" label="Errors" width="110" sortable />
        <el-table-column prop="total_tokens" label="Tokens" width="140" sortable />
        <el-table-column prop="cost" label="Cost ($)" sortable>
          <template #default="{ row }">{{ money(row.cost) }}</template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-row :gutter="12" class="mt">
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat-label">Total requests</div>
          <div class="stat-value">{{ overview?.total.total_requests ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat-label">Total tokens</div>
          <div class="stat-value">{{ overview?.total.total_tokens ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat-label">Total cost ($)</div>
          <div class="stat-value">{{ money(overview?.total.cost) }}</div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.stat-label { font-size: 12px; color: #909399; }
.stat-value { font-size: 20px; font-weight: 600; margin-top: 4px; }
.dim { font-size: 13px; font-weight: 400; color: #909399; }
.mt { margin-top: 12px; }
</style>
