<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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

function humanNum(n?: number): string {
  if (n == null) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return +(n / 1e3).toFixed(1) + 'k'
  return String(n)
}

const cards = computed(() => {
  const m = me.value
  const t = overview.value?.today
  const callBal = m?.calls_balance ?? 0
  const creditBal = m?.credit_balance ?? 0
  return [
    {
      label: 'Calls balance', value: String(callBal),
      sub: `spent ${m?.calls_spent ?? 0} · earned ${m?.calls_earned ?? 0}`,
      neg: callBal < 0, color: callBal < 0 ? '#c9808a' : '#6f9a86', bg: callBal < 0 ? 'rgba(201,128,138,.16)' : 'rgba(111,154,134,.15)', icon: 'Odometer',
    },
    {
      label: 'Credit balance ($)', value: creditBal.toFixed(4),
      sub: `spent $${(m?.credit_spent ?? 0).toFixed(4)} · earned $${(m?.credit_earned ?? 0).toFixed(4)}`,
      neg: creditBal < 0, color: creditBal < 0 ? '#c9808a' : '#6f9a86', bg: creditBal < 0 ? 'rgba(201,128,138,.16)' : 'rgba(111,154,134,.15)', icon: 'Wallet',
    },
    {
      label: 'Quota used ($)', value: money(m?.quota_used),
      sub: `limit ${m?.quota_limit ? money(m.quota_limit) : 'unlimited'}`,
      neg: false, color: '#b18a4f', bg: 'rgba(177,138,79,.16)', icon: 'Coin',
    },
    {
      label: 'Today requests', value: humanNum(t?.total_requests),
      sub: `tokens ${humanNum(t?.total_tokens)} · $${(t?.cost ?? 0).toFixed(4)}`,
      neg: false, color: '#7a93b8', bg: 'rgba(122,147,184,.16)', icon: 'Connection',
    },
  ]
})

// 每日上传/下载（completion = total - prompt）
function upload(p: { prompt_tokens?: number }) {
  return humanNum(p.prompt_tokens ?? 0)
}
function download(p: { total_tokens?: number; prompt_tokens?: number }) {
  return humanNum((p.total_tokens ?? 0) - (p.prompt_tokens ?? 0))
}

const totalCards = computed(() => {
  const t = overview.value?.total
  return [
    { label: 'Total requests', value: humanNum(t?.total_requests), icon: 'Connection' },
    { label: 'Total tokens', value: humanNum(t?.total_tokens), sub: `${humanNum(t?.prompt_tokens)} / ${humanNum(t?.completion_tokens)}`, icon: 'ChatDotRound' },
    { label: 'Total cost ($)', value: money(t?.cost), icon: 'Money' },
  ]
})

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <el-row :gutter="14">
      <el-col v-for="s in cards" :key="s.label" :xs="12" :md="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-row">
            <div class="stat-icon" :style="{ background: s.bg, color: s.color }">
              <el-icon :size="20"><component :is="s.icon" /></el-icon>
            </div>
            <div class="stat-meta">
              <div class="stat-num" :class="{ neg: s.neg }">{{ s.value }}</div>
              <div class="stat-label">{{ s.label }}</div>
              <div class="stat-sub">{{ s.sub }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header><span>Last 7 days</span></template>
      <el-table :data="overview?.week ?? []" size="small">
        <el-table-column prop="date" label="Date" width="130" />
        <el-table-column prop="requests" label="Requests" width="100" align="right" sortable />
        <el-table-column prop="success" label="Success" width="100" align="right" sortable />
        <el-table-column label="Errors" width="90" align="right" sortable>
          <template #default="{ row }"><span :class="{ neg: row.errors > 0 }">{{ row.errors }}</span></template>
        </el-table-column>
        <el-table-column label="Upload" width="110" align="right" sortable>
          <template #default="{ row }">{{ upload(row) }}</template>
        </el-table-column>
        <el-table-column label="Download" width="110" align="right" sortable>
          <template #default="{ row }">{{ download(row) }}</template>
        </el-table-column>
        <el-table-column label="Cost ($)" align="right" sortable>
          <template #default="{ row }">{{ money(row.cost) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!overview?.week?.length" description="No data yet" :image-size="60" />
    </el-card>

    <el-row :gutter="14" class="mt">
      <el-col v-for="s in totalCards" :key="s.label" :xs="8">
        <el-card shadow="never" class="stat-card">
          <div class="stat-row">
            <div class="stat-icon plain"><el-icon :size="18"><component :is="s.icon" /></el-icon></div>
            <div class="stat-meta">
              <div class="stat-num">{{ s.value }}</div>
              <div class="stat-label">{{ s.label }}</div>
              <div v-if="s.sub" class="stat-sub">{{ s.sub }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.stat-card { margin-bottom: 12px; }
.stat-row { display: flex; align-items: center; gap: 10px; }
.stat-icon { width: 38px; height: 38px; border-radius: 10px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.stat-icon.plain { width: 38px; height: 38px; background: var(--ll-soft); color: var(--ll-muted); }
.stat-meta { min-width: 0; }
.stat-num { font-size: 20px; font-weight: 700; color: var(--ll-ink); line-height: 1.2; white-space: nowrap; }
.stat-num.neg { color: #c9808a; }
.stat-label { color: var(--ll-muted); font-size: 13px; margin-top: 2px; white-space: nowrap; }
.stat-sub { color: var(--ll-muted); font-size: 11.5px; margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.neg { color: #c9808a; }
.mt { margin-top: 12px; }
</style>
