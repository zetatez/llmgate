<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { TeamUsage, UserModelUsage } from '../../api'

const rows = ref<TeamUsage[]>([])
const byModel = ref<UserModelUsage[]>([])
const loading = ref(false)
const days = ref(7)

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

const totals = computed(() =>
  rows.value.reduce((s, r) => ({
    users: s.users + 1,
    requests: s.requests + r.requests,
    success: s.success + r.success,
    prompt: s.prompt + r.prompt_tokens,
    completion: s.completion + r.completion_tokens,
    total: s.total + r.total_tokens,
    cost: s.cost + r.cost,
  }), { users: 0, requests: 0, success: 0, prompt: 0, completion: 0, total: 0, cost: 0 }),
)

function modelsFor(userID: number): UserModelUsage[] {
  return byModel.value.filter((m) => m.user_id === userID).sort((a, b) => b.total_tokens - a.total_tokens)
}

async function load() {
  loading.value = true
  try {
    const [u, m] = await Promise.all([portalApi.teamUsage(days.value), portalApi.teamUsageByModel(days.value)])
    rows.value = u
    byModel.value = m
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <el-card shadow="never" class="toolbar-card">
      <div class="toolbar">
        <el-radio-group v-model="days" size="small" @change="load">
          <el-radio-button :value="1">Today</el-radio-button>
          <el-radio-button :value="7">7 days</el-radio-button>
          <el-radio-button :value="30">30 days</el-radio-button>
          <el-radio-button :value="0">All</el-radio-button>
        </el-radio-group>
        <span class="hint">Team usage — visible to everyone</span>
      </div>
    </el-card>

    <el-row :gutter="14" class="mt">
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.requests) }}</div><div class="sum-label">Requests ({{ humanNum(totals.success) }} ok)</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.prompt) }}</div><div class="sum-label">Upload tokens</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.completion) }}</div><div class="sum-label">Download tokens</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">${{ totals.cost.toFixed(2) }}</div><div class="sum-label">Total cost</div></el-card></el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <el-table :data="rows" size="small" row-key="user_id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <el-table :data="modelsFor(row.user_id)" size="small" class="inner-table">
              <el-table-column prop="model" label="Model" min-width="180" show-overflow-tooltip />
              <el-table-column prop="requests" label="Requests" width="110" align="right" />
              <el-table-column label="Upload tokens" width="140" align="right">
                <template #default="{ row: m }">{{ humanNum(m.prompt_tokens) }}</template>
              </el-table-column>
              <el-table-column label="Download tokens" width="150" align="right">
                <template #default="{ row: m }">{{ humanNum(m.completion_tokens) }}</template>
              </el-table-column>
              <el-table-column label="Total tokens" width="130" align="right">
                <template #default="{ row: m }">{{ humanNum(m.total_tokens) }}</template>
              </el-table-column>
              <el-table-column label="Cost ($)" width="110" align="right">
                <template #default="{ row: m }">{{ money(m.cost) }}</template>
              </el-table-column>
            </el-table>
          </template>
        </el-table-column>
        <el-table-column label="User" min-width="160">
          <template #default="{ row }">{{ row.user_name || `#${row.user_id}` }}</template>
        </el-table-column>
        <el-table-column prop="requests" label="Requests" width="110" align="right" sortable />
        <el-table-column prop="success" label="Success" width="100" align="right" sortable />
        <el-table-column label="Upload tokens" width="140" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.prompt_tokens) }}</template>
        </el-table-column>
        <el-table-column label="Download tokens" width="150" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.completion_tokens) }}</template>
        </el-table-column>
        <el-table-column label="Total tokens" width="130" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.total_tokens) }}</template>
        </el-table-column>
        <el-table-column label="Cost ($)" width="110" align="right" sortable>
          <template #default="{ row }">{{ money(row.cost) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !rows.length" description="No usage in the selected range" />
    </el-card>
  </div>
</template>

<style scoped>
.toolbar-card { background: transparent; border: none; box-shadow: none; padding: 0; }
.toolbar { display: flex; align-items: center; gap: 12px; }
.hint { font-size: 12px; color: var(--ll-muted); }
.sum { margin-bottom: 14px; }
.sum-num { font-size: 22px; font-weight: 700; color: var(--ll-ink); }
.sum-label { font-size: 12px; color: var(--ll-muted); margin-top: 2px; }
.inner-table { margin: 4px 12px; }
.mt { margin-top: 14px; }
</style>
