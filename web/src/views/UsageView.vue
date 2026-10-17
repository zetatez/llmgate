<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { logsApi, usersApi } from '../api'
import type { User, UserModelUsage } from '../api'

const usage = ref<UserModelUsage[]>([])
const users = ref<User[]>([])
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

interface Row {
  user_id: number
  user_name: string
  requests: number
  prompt: number
  completion: number
  cost: number
  calls_spent: number
  calls_earned: number
  calls_balance: number
  credit_spent: number
  credit_earned: number
  credit_balance: number
  debt_floor_calls: number
  debt_floor_credit: number
  models: UserModelUsage[]
}

const rows = computed<Row[]>(() => {
  const m = new Map<number, Row>()
  for (const u of users.value) {
    m.set(u.id, {
      user_id: u.id, user_name: u.name || u.username,
      requests: 0, prompt: 0, completion: 0, cost: 0,
      calls_spent: u.calls_spent, calls_earned: u.calls_earned, calls_balance: u.calls_balance,
      credit_spent: u.credit_spent, credit_earned: u.credit_earned, credit_balance: u.credit_balance,
      debt_floor_calls: u.debt_floor_calls, debt_floor_credit: u.debt_floor_credit,
      models: [],
    })
  }
  for (const r of usage.value) {
    let row = m.get(r.user_id)
    if (!row) {
      row = {
        user_id: r.user_id, user_name: r.user_name || `#${r.user_id}`,
        requests: 0, prompt: 0, completion: 0, cost: 0,
        calls_spent: 0, calls_earned: 0, calls_balance: 0,
        credit_spent: 0, credit_earned: 0, credit_balance: 0,
        debt_floor_calls: 0, debt_floor_credit: 0, models: [],
      }
      m.set(r.user_id, row)
    }
    row.requests += r.requests
    row.prompt += r.prompt_tokens
    row.completion += r.completion_tokens
    row.cost += r.cost
    row.models.push(r)
  }
  return [...m.values()].sort((a, b) => b.cost - a.cost || b.calls_spent - a.calls_spent)
})

const totals = computed(() =>
  rows.value.reduce((s, u) => ({
    users: s.users + 1,
    requests: s.requests + u.requests,
    prompt: s.prompt + u.prompt,
    completion: s.completion + u.completion,
    cost: s.cost + u.cost,
    callsBalance: s.callsBalance + u.calls_balance,
    creditBalance: s.creditBalance + u.credit_balance,
  }), { users: 0, requests: 0, prompt: 0, completion: 0, cost: 0, callsBalance: 0, creditBalance: 0 }),
)

async function load() {
  loading.value = true
  try {
    const [u, us] = await Promise.all([logsApi.usageByUserModel(days.value), usersApi.list()])
    usage.value = u
    users.value = us
  } finally {
    loading.value = false
  }
}

function modelRows(r: Row): UserModelUsage[] {
  return [...r.models].sort((a, b) => b.total_tokens - a.total_tokens)
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
        <span class="hint">Usage is per selected range; balances are running totals.</span>
      </div>
    </el-card>

    <el-row :gutter="14" class="mt">
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.requests) }}</div><div class="sum-label">Requests</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(totals.prompt) }} / {{ humanNum(totals.completion) }}</div><div class="sum-label">Upload / Download tokens</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num" :class="{ neg: totals.callsBalance < 0 }">{{ totals.callsBalance }}</div><div class="sum-label">Calls balance (all users)</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num" :class="{ neg: totals.creditBalance < 0 }">${{ totals.creditBalance.toFixed(2) }}</div><div class="sum-label">Credit balance (all users)</div></el-card></el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <el-table :data="rows" size="small" row-key="user_id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="expand-wrap">
              <el-table v-if="modelRows(row).length" :data="modelRows(row)" size="small" style="width: 100%">
                <el-table-column prop="model" label="Model" min-width="200" show-overflow-tooltip />
                <el-table-column prop="requests" label="Requests" width="100" align="right" />
                <el-table-column label="Upload / Download" width="200" align="right">
                  <template #default="{ row: m }">{{ humanNum(m.prompt_tokens) }} / {{ humanNum(m.completion_tokens) }}</template>
                </el-table-column>
                <el-table-column label="Total" width="110" align="right">
                  <template #default="{ row: m }">{{ humanNum(m.total_tokens) }}</template>
                </el-table-column>
                <el-table-column label="Cost ($)" width="110" align="right">
                  <template #default="{ row: m }">{{ money(m.cost) }}</template>
                </el-table-column>
              </el-table>
              <el-empty v-else description="No usage in this range" :image-size="40" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="User" min-width="170">
          <template #default="{ row }">{{ row.user_name || `#${row.user_id}` }}</template>
        </el-table-column>
        <el-table-column prop="requests" label="Requests" width="100" align="right" sortable />
        <el-table-column label="Upload" width="110" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.prompt) }}</template>
        </el-table-column>
        <el-table-column label="Download" width="110" align="right" sortable>
          <template #default="{ row }">{{ humanNum(row.completion) }}</template>
        </el-table-column>
        <el-table-column label="Cost ($)" width="100" align="right" sortable>
          <template #default="{ row }">{{ money(row.cost) }}</template>
        </el-table-column>
        <el-table-column label="Calls (bal)" width="110" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.calls_balance < 0 }">{{ row.calls_balance }}</span>
            <div class="sub">{{ row.calls_spent }} / {{ row.calls_earned }}</div>
          </template>
        </el-table-column>
        <el-table-column label="Credit (bal)" width="120" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.credit_balance < 0 }">${{ row.credit_balance.toFixed(4) }}</span>
            <div class="sub">${{ row.credit_spent.toFixed(4) }} / ${{ row.credit_earned.toFixed(4) }}</div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !rows.length" description="No users yet" />
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
.neg { color: #c9808a; }
.sub { font-size: 11px; color: var(--ll-muted); }
.expand-wrap { padding: 8px 16px 12px; }
.mt { margin-top: 14px; }
</style>
