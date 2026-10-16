<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { logsApi } from '../api'
import type { UserModelUsage } from '../api'

const raw = ref<UserModelUsage[]>([])
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

interface UserRow {
  user_id: number
  user_name: string
  requests: number
  prompt: number
  completion: number
  total: number
  cost: number
  models: UserModelUsage[]
}

const perUser = computed<UserRow[]>(() => {
  const m = new Map<number, UserRow>()
  for (const r of raw.value) {
    let u = m.get(r.user_id)
    if (!u) {
      u = { user_id: r.user_id, user_name: r.user_name, requests: 0, prompt: 0, completion: 0, total: 0, cost: 0, models: [] }
      m.set(r.user_id, u)
    }
    u.requests += r.requests
    u.prompt += r.prompt_tokens
    u.completion += r.completion_tokens
    u.total += r.total_tokens
    u.cost += r.cost
    u.models.push(r)
  }
  return [...m.values()].sort((a, b) => b.total - a.total)
})

const totals = computed(() =>
  perUser.value.reduce((s, u) => ({
    users: s.users + 1,
    requests: s.requests + u.requests,
    prompt: s.prompt + u.prompt,
    completion: s.completion + u.completion,
    total: s.total + u.total,
    cost: s.cost + u.cost,
  }), { users: 0, requests: 0, prompt: 0, completion: 0, total: 0, cost: 0 }),
)

const rangeLabel = computed(() => (days.value === 0 ? 'All time' : days.value === 1 ? 'Today' : `Last ${days.value} days`))

async function load() {
  loading.value = true
  try {
    raw.value = await logsApi.usageByUserModel(days.value)
  } finally {
    loading.value = false
  }
}

function modelRows(u: UserRow): UserModelUsage[] {
  return [...u.models].sort((a, b) => b.total_tokens - a.total_tokens)
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
        <span class="range-hint">{{ rangeLabel }}</span>
      </div>
    </el-card>

    <el-row :gutter="14" class="mt">
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ totals.users }}</div><div class="sum-label">Users</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.requests) }}</div><div class="sum-label">Requests</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.total) }}</div><div class="sum-label">Tokens (in {{ humanNum(totals.prompt) }} / out {{ humanNum(totals.completion) }})</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">${{ totals.cost.toFixed(2) }}</div><div class="sum-label">Cost</div></el-card></el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <el-table :data="perUser" size="small" row-key="user_id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <el-table :data="modelRows(row)" size="small" class="inner-table">
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
        <el-table-column label="User" min-width="180">
          <template #default="{ row }">{{ row.user_name || `#${row.user_id}` }}</template>
        </el-table-column>
        <el-table-column prop="requests" label="Requests" width="110" align="right" sortable />
        <el-table-column label="Upload tokens" width="150" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.prompt) }}</template>
        </el-table-column>
        <el-table-column label="Download tokens" width="160" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.completion) }}</template>
        </el-table-column>
        <el-table-column label="Total tokens" width="140" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.total) }}</template>
        </el-table-column>
        <el-table-column label="Cost ($)" width="120" align="right" sortable>
          <template #default="{ row }">{{ money(row.cost) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !perUser.length" description="No usage in the selected range" />
    </el-card>
  </div>
</template>

<style scoped>
.toolbar-card {
  background: transparent;
  border: none;
  box-shadow: none;
  padding: 0;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
}
.range-hint {
  font-size: 12px;
  color: var(--ll-muted);
}
.sum {
  margin-bottom: 14px;
}
.sum-num {
  font-size: 22px;
  font-weight: 700;
  color: var(--ll-ink);
}
.sum-label {
  font-size: 12px;
  color: var(--ll-muted);
  margin-top: 2px;
}
.inner-table {
  margin: 4px 12px;
}
.mt {
  margin-top: 14px;
}
</style>
