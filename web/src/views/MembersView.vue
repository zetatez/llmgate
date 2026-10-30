<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { membersApi, portalApi } from '../api'
import type { MemberStat } from '../api'
import { fmtDate } from '../utils/format'

const props = defineProps<{ scope?: 'admin' | 'user' }>()
const isUser = computed(() => props.scope === 'user')

const list = ref<MemberStat[]>([])
const loading = ref(false)
const myID = ref(0)
const search = ref('')

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

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return list.value
  return list.value.filter((m) => m.username.toLowerCase().includes(q) || (m.name || '').toLowerCase().includes(q))
})

// 全站 token 总量，用于计算成员占比条
const tokenTotal = computed(() => list.value.reduce((s, m) => s + m.in_spent + m.out_spent, 0) || 1)

function tokenShare(m: MemberStat): number {
  return ((m.in_spent + m.out_spent) / tokenTotal.value) * 100
}

const summary = computed(() => {
  const weekAgo = Date.now() / 1000 - 7 * 86400
  return {
    members: list.value.length,
    joinedThisWeek: list.value.filter((m) => m.created_at >= weekAgo).length,
    requests: list.value.reduce((s, m) => s + m.calls_spent, 0),
    tokens: list.value.reduce((s, m) => s + m.in_spent + m.out_spent, 0),
    spent: list.value.reduce((s, m) => s + m.credit_spent, 0),
  }
})

async function load() {
  loading.value = true
  try {
    if (isUser.value) {
      const [members, me] = await Promise.all([portalApi.members(), portalApi.me()])
      list.value = members
      myID.value = me.id
    } else {
      list.value = await membersApi.list()
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load members')
  } finally {
    loading.value = false
  }
}

function rowClass({ row }: { row: MemberStat }): string {
  return isUser.value && row.id === myID.value ? 'me-row' : ''
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <el-row :gutter="14">
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ summary.members }}</div><div class="sum-label">Members</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ summary.joinedThisWeek }}</div><div class="sum-label">Joined this week</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(summary.requests) }}</div><div class="sum-label">Total requests</div></el-card></el-col>
      <el-col :xs="12" :md="6"><el-card shadow="never" class="sum"><div class="sum-num">{{ humanNum(summary.tokens) }}</div><div class="sum-label">Total tokens · ${{ summary.spent.toFixed(2) }} spent</div></el-card></el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header>
        <div class="head">
          <span>Members</span>
          <el-input v-model="search" placeholder="Search username" clearable size="small" class="search" />
        </div>
      </template>
      <el-table :data="filtered" size="small" :row-class-name="rowClass" :default-sort="{ prop: 'calls_spent', order: 'descending' }">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column label="Member" min-width="220">
          <template #default="{ row }">
            <div class="member">
              <span class="u">{{ row.username }}</span>
              <span v-if="isUser && row.id === myID" class="you">you</span>
            </div>
            <div v-if="row.name && row.name !== row.username" class="sub">{{ row.name }}</div>
            <div class="bar"><div class="bar-inner" :style="{ width: tokenShare(row) + '%' }"></div></div>
          </template>
        </el-table-column>
        <el-table-column label="Share" width="80" align="right" sortable :sort-by="(row: MemberStat) => row.in_spent + row.out_spent">
          <template #default="{ row }">{{ tokenShare(row).toFixed(1) }}%</template>
        </el-table-column>
        <el-table-column label="Joined" width="120">
          <template #default="{ row }">{{ fmtDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="calls_spent" label="Requests" width="110" align="right" sortable />
        <el-table-column label="Upload / Download" width="180" align="right">
          <template #default="{ row }">{{ humanNum(row.in_spent) }} / {{ humanNum(row.out_spent) }}</template>
        </el-table-column>
        <el-table-column label="Spent ($)" width="120" align="right" sortable>
          <template #default="{ row }">{{ money(row.credit_spent) }}</template>
        </el-table-column>
        <el-table-column label="Contributed" width="170" align="right">
          <template #default="{ row }">
            <span>{{ row.calls_earned }} calls</span>
            <div class="sub">{{ humanNum(row.in_served) }} / {{ humanNum(row.out_served) }} · ${{ money(row.credit_earned) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="Calls (bal)" width="110" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.calls_balance < 0 }">{{ row.calls_balance }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Credit ($)" width="120" align="right" sortable>
          <template #default="{ row }">
            <span :class="{ neg: row.credit_balance < 0 }">{{ money(row.credit_balance) }}</span>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !filtered.length" description="No members yet" />
    </el-card>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 12px; }
.mt { margin-top: 14px; }
.sum { margin-bottom: 6px; }
.sum-num { font-size: 22px; font-weight: 700; color: var(--ll-ink); }
.sum-label { font-size: 12px; color: var(--ll-muted); margin-top: 2px; }
.head { display: flex; justify-content: space-between; align-items: center; }
.search { width: 240px; }
.member { display: flex; align-items: center; gap: 8px; }
.u { font-weight: 600; color: var(--ll-ink); }
.you { font-size: 10px; padding: 0 5px; border-radius: 4px; background: rgba(139, 147, 214, .2); color: #aeb5ee; }
.sub { font-size: 11px; color: var(--ll-muted); }
.bar { height: 4px; border-radius: 2px; background: var(--ll-soft); margin-top: 5px; overflow: hidden; max-width: 260px; }
.bar-inner { height: 100%; border-radius: 2px; background: linear-gradient(90deg, #8b93d6, #b5a7dd); }
.neg { color: #c9808a; }
:deep(.me-row) { background: rgba(139, 147, 214, .08); }
</style>
