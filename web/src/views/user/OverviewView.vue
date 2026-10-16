<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { portalApi } from '../../api'
import type { PortalUser, UserOverview } from '../../api'
import { User, Coin, Connection, Money } from '@element-plus/icons-vue'

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

const cards = computed(() => [
  { label: 'Account', value: me.value?.username || '—', sub: me.value?.name || '', icon: User, color: 'var(--el-color-primary)', bg: 'rgba(124,134,214,.15)' },
  { label: 'Quota used ($)', value: money(me.value?.quota_used), sub: `/ ${me.value?.quota_limit ? money(me.value.quota_limit) : 'unlimited'}`, icon: Coin, color: '#6f9a86', bg: 'rgba(111,154,134,.15)' },
  { label: 'Today requests', value: humanNum(overview.value?.today.total_requests), sub: `${overview.value?.today.total_requests ?? 0}`, icon: Connection, color: '#7a93b8', bg: 'var(--ll-soft)' },
  { label: 'Today cost ($)', value: money(overview.value?.today.cost), sub: '', icon: Money, color: '#b18a4f', bg: 'rgba(177,138,79,.16)' },
])

const totalCards = computed(() => [
  { label: 'Total requests', value: humanNum(overview.value?.total.total_requests), icon: 'Connection' },
  { label: 'Total tokens', value: humanNum(overview.value?.total.total_tokens), icon: 'ChatDotRound' },
  { label: 'Total cost ($)', value: money(overview.value?.total.cost), icon: 'Money' },
])

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
              <div class="stat-num">{{ s.value }}<span v-if="s.sub" class="sub">{{ s.sub }}</span></div>
              <div class="stat-label">{{ s.label }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="mt">
      <template #header><span>Last 7 days</span></template>
      <el-table :data="overview?.week ?? []" size="small">
        <el-table-column prop="date" label="Date" width="140" />
        <el-table-column prop="requests" label="Requests" width="110" sortable />
        <el-table-column prop="success" label="Success" width="110" sortable />
        <el-table-column label="Errors" width="100" sortable>
          <template #default="{ row }">
            <span :class="{ err: row.errors > 0 }">{{ row.errors }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="total_tokens" label="Tokens" width="140" sortable />
        <el-table-column label="Cost ($)" sortable>
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
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.stat-card {
  margin-bottom: 14px;
}
.stat-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 4px 2px;
}
.stat-icon {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-icon.plain {
  width: 38px;
  height: 38px;
  background: var(--ll-soft);
  color: var(--ll-muted);
}
.stat-meta {
  min-width: 0;
}
.stat-num {
  font-size: 22px;
  font-weight: 700;
  color: var(--ll-ink);
  line-height: 1.2;
  white-space: nowrap;
}
.sub {
  font-size: 12px;
  font-weight: 400;
  color: var(--ll-muted);
  margin-left: 4px;
}
.stat-label {
  color: var(--ll-muted);
  font-size: 13px;
  margin-top: 2px;
  white-space: nowrap;
}
.err {
  color: #e11d48;
  font-weight: 600;
}
.mt {
  margin-top: 14px;
}
</style>