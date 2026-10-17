<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { PortalUser, TeamUsage, UserModelUsage } from '../../api'

const me = ref<PortalUser | null>(null)
const team = ref<TeamUsage[]>([])
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

const myCards = computed(() => {
  const m = me.value
  const callBal = m?.calls_balance ?? 0
  const creditBal = m?.credit_balance ?? 0
  return [
    {
      label: 'Calls balance', value: String(callBal),
      sub: `spent ${m?.calls_spent ?? 0} · earned ${m?.calls_earned ?? 0} · floor ${m?.debt_floor_calls ?? 0}`,
      neg: callBal < 0, color: callBal < 0 ? '#c9808a' : '#6f9a86', bg: callBal < 0 ? 'rgba(201,128,138,.16)' : 'rgba(111,154,134,.15)', icon: 'Odometer',
    },
    {
      label: 'Credit balance ($)', value: creditBal.toFixed(4),
      sub: `spent $${(m?.credit_spent ?? 0).toFixed(4)} · earned $${(m?.credit_earned ?? 0).toFixed(4)} · floor $${(m?.debt_floor_credit ?? 0).toFixed(2)}`,
      neg: creditBal < 0, color: creditBal < 0 ? '#c9808a' : '#6f9a86', bg: creditBal < 0 ? 'rgba(201,128,138,.16)' : 'rgba(111,154,134,.15)', icon: 'Wallet',
    },
    {
      label: 'Upload tokens (used)', value: humanNum(m?.in_spent),
      sub: `served to others ${humanNum(m?.in_served)}`,
      neg: false, color: '#7a93b8', bg: 'rgba(122,147,184,.16)', icon: 'Upload',
    },
    {
      label: 'Download tokens (used)', value: humanNum(m?.out_spent),
      sub: `served to others ${humanNum(m?.out_served)}`,
      neg: false, color: '#b18a4f', bg: 'rgba(177,138,79,.16)', icon: 'Download',
    },
  ]
})

function modelsFor(userID: number): UserModelUsage[] {
  return byModel.value.filter((m) => m.user_id === userID).sort((a, b) => b.total_tokens - a.total_tokens)
}

async function loadTeam() {
  const [t, m] = await Promise.all([portalApi.teamUsage(days.value), portalApi.teamUsageByModel(days.value)])
  team.value = t
  byModel.value = m
}

async function load() {
  loading.value = true
  try {
    const [u] = await Promise.all([portalApi.me(), loadTeam()])
    me.value = u
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <div class="section-title">My credits</div>
    <el-row :gutter="14">
      <el-col v-for="s in myCards" :key="s.label" :xs="12" :md="6">
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

    <div class="section-title mt">Team</div>
    <el-card shadow="never" class="toolbar-card">
      <div class="toolbar">
        <el-radio-group v-model="days" size="small" @change="loadTeam">
          <el-radio-button :value="1">Today</el-radio-button>
          <el-radio-button :value="7">7 days</el-radio-button>
          <el-radio-button :value="30">30 days</el-radio-button>
          <el-radio-button :value="0">All</el-radio-button>
        </el-radio-group>
        <span class="hint">Consumption and contribution of every member — visible to all.</span>
      </div>
    </el-card>

    <el-card shadow="never" class="mt">
      <el-table :data="team" size="small" row-key="user_id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <el-table :data="modelsFor(row.user_id)" size="small" class="inner-table">
              <el-table-column prop="model" label="Model" min-width="180" show-overflow-tooltip />
              <el-table-column prop="requests" label="Requests" width="110" align="right" />
              <el-table-column label="Upload" width="120" align="right"><template #default="{ row: m }">{{ humanNum(m.prompt_tokens) }}</template></el-table-column>
              <el-table-column label="Download" width="120" align="right"><template #default="{ row: m }">{{ humanNum(m.completion_tokens) }}</template></el-table-column>
              <el-table-column label="Cost ($)" width="110" align="right"><template #default="{ row: m }">{{ money(m.cost) }}</template></el-table-column>
            </el-table>
          </template>
        </el-table-column>
        <el-table-column label="User" min-width="150">
          <template #default="{ row }">{{ row.user_name || `#${row.user_id}` }}</template>
        </el-table-column>
        <el-table-column prop="requests" label="Requests" width="100" align="right" sortable />
        <el-table-column prop="success" label="Success" width="90" align="right" sortable />
        <el-table-column label="Upload" width="100" align="right" sortable><template #default="{ row }">{{ humanNum(row.prompt_tokens) }}</template></el-table-column>
        <el-table-column label="Download" width="110" align="right" sortable><template #default="{ row }">{{ humanNum(row.completion_tokens) }}</template></el-table-column>
        <el-table-column label="Cost ($)" width="100" align="right" sortable><template #default="{ row }">{{ money(row.cost) }}</template></el-table-column>
        <el-table-column label="Contributed" width="130" align="right">
          <template #default="{ row }">
            <span>{{ row.earned_calls }} calls</span>
            <div class="sub">{{ humanNum(row.earned_in_tokens) }} / {{ humanNum(row.earned_out_tokens) }} · ${{ row.earned_cost.toFixed(4) }}</div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !team.length" description="No usage in the selected range" />
    </el-card>
  </div>
</template>

<style scoped>
.section-title { font-size: 14px; font-weight: 700; color: var(--ll-ink); margin-bottom: 12px; }
.stat-card { margin-bottom: 14px; }
.stat-row { display: flex; align-items: center; gap: 14px; padding: 4px 2px; }
.stat-icon { width: 46px; height: 46px; border-radius: 12px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.stat-meta { min-width: 0; }
.stat-num { font-size: 22px; font-weight: 700; color: var(--ll-ink); line-height: 1.2; white-space: nowrap; }
.stat-num.neg { color: #c9808a; }
.stat-label { color: var(--ll-muted); font-size: 13px; margin-top: 2px; }
.stat-sub { color: var(--ll-muted); font-size: 11.5px; margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.toolbar-card { background: transparent; border: none; box-shadow: none; padding: 0; }
.toolbar { display: flex; align-items: center; gap: 12px; }
.hint { font-size: 12px; color: var(--ll-muted); }
.sub { font-size: 11px; color: var(--ll-muted); }
.inner-table { margin: 4px 12px; }
.mt { margin-top: 16px; }
</style>
