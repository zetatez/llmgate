<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { channelsApi, logsApi, portalApi, usersApi } from '../api'
import type { RequestLog, User } from '../api'

const props = defineProps<{ scope?: 'admin' | 'user' }>()
const isUser = computed(() => props.scope === 'user')

const list = ref<RequestLog[]>([])
const loading = ref(false)
const status = ref('')
const displayModel = ref('')
const range = ref<[Date, Date] | null>(null)
const channelId = ref<number | ''>('')
const userId = ref<number | ''>('')
const channels = ref<Array<{ id: number; name: string }>>([])
const users = ref<User[]>([])
const page = ref(1)
const size = ref(50)
const total = ref(0)
const detail = reactive<{ visible: boolean; row: RequestLog | null }>({ visible: false, row: null })

function fmtTime(ts: number) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function money(v: number) {
  return (v ?? 0).toFixed(4)
}

function baseParams(): Record<string, string | number> {
  const params: Record<string, string | number> = {}
  if (status.value) params.status = status.value
  if (displayModel.value) params.display_model = displayModel.value
  if (channelId.value) params.channel_id = channelId.value
  if (!isUser.value && userId.value) params.user_id = userId.value
  if (range.value && range.value.length === 2) {
    params.from = Math.floor(range.value[0].getTime() / 1000)
    // 结束时间包含当前分钟
    params.to = Math.floor(range.value[1].getTime() / 1000) + 1
  }
  return params
}

async function search() {
  page.value = 1
  await load()
}

function resetFilters() {
  status.value = ''
  displayModel.value = ''
  range.value = null
  channelId.value = ''
  userId.value = ''
  page.value = 1
  load()
}

async function load() {
  loading.value = true
  try {
    const params = { ...baseParams(), limit: size.value, offset: (page.value - 1) * size.value }
    list.value = isUser.value ? await portalApi.logs(params as any) : await logsApi.list(params)
    total.value = isUser.value ? await portalApi.logsCount(params as any) : await logsApi.count(baseParams())
  } finally {
    loading.value = false
  }
}

async function loadFilters() {
  try {
    channels.value = isUser.value ? await portalApi.channels() : await channelsApi.list()
    if (!isUser.value) users.value = await usersApi.list()
  } catch {
    /* filters are optional */
  }
}

function onSizeChange() {
  page.value = 1
  load()
}

function openDetails(row: RequestLog) {
  detail.row = row
  detail.visible = true
}

onMounted(async () => {
  await Promise.all([load(), loadFilters()])
})
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="filters">
        <el-input v-model="displayModel" placeholder="Model" clearable style="width: 160px" @keyup.enter="search" />
        <el-select v-model="status" placeholder="Status" clearable style="width: 110px">
          <el-option label="Success" value="success" />
          <el-option label="Failed" value="error" />
        </el-select>
        <el-select v-model="channelId" placeholder="Channel" clearable filterable style="width: 140px">
          <el-option v-for="c in channels" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <el-select v-if="!isUser" v-model="userId" placeholder="User" clearable filterable style="width: 140px">
          <el-option v-for="u in users" :key="u.id" :label="u.username || u.name" :value="u.id" />
        </el-select>
        <el-date-picker
          v-model="range"
          type="datetimerange"
          start-placeholder="From"
          end-placeholder="To"
          range-separator="–"
          style="width: 330px"
        />
        <el-button type="primary" @click="search">Search</el-button>
        <el-button @click="resetFilters">Reset</el-button>
        <el-select v-model="size" style="width: 120px" @change="onSizeChange">
          <el-option label="50 / page" :value="50" />
          <el-option label="100 / page" :value="100" />
          <el-option label="200 / page" :value="200" />
        </el-select>
      </div>
    </template>
    <el-table
      v-loading="loading"
      :data="list"
      size="small"
      max-height="70vh"
      class="log-table"
      @row-click="openDetails"
    >
      <el-table-column label="Time" width="165" class-name="nowrap">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="display_model" label="Model" width="200" show-overflow-tooltip />
      <el-table-column v-if="!isUser" label="User" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.user_name || '—' }}</template>
      </el-table-column>
      <el-table-column label="Channel" width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.channel_name || `#${row.channel_id}` }}</template>
      </el-table-column>
      <el-table-column label="Upstream" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          {{ row.channel_name ? `${row.channel_name}/${row.upstream_model}` : row.upstream_model || '—' }}
        </template>
      </el-table-column>
      <el-table-column label="Tokens" width="90" class-name="nowrap">
        <template #default="{ row }">{{ row.total_tokens }}</template>
      </el-table-column>
      <el-table-column label="Cost ($)" width="100" class-name="nowrap">
        <template #default="{ row }">{{ money(row.cost) }}</template>
      </el-table-column>
      <el-table-column prop="latency_ms" label="Latency (ms)" width="100" class-name="nowrap" />
      <el-table-column label="Status" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error_code" label="Error" min-width="180" show-overflow-tooltip />
    </el-table>

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

    <el-drawer v-model="detail.visible" title="Request details" size="520px">
      <el-descriptions v-if="detail.row" :column="1" border size="small">
        <el-descriptions-item label="Time">{{ fmtTime(detail.row.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="Model">{{ detail.row.display_model }}</el-descriptions-item>
        <el-descriptions-item label="Upstream">{{ detail.row.upstream_model || '—' }}</el-descriptions-item>
        <el-descriptions-item label="Channel">{{ detail.row.channel_name || `#${detail.row.channel_id}` }}</el-descriptions-item>
        <el-descriptions-item v-if="!isUser" label="User">{{ detail.row.user_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="Status">
          <el-tag :type="detail.row.status === 'success' ? 'success' : 'danger'" size="small">{{ detail.row.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="Tokens">{{ detail.row.total_tokens }} ({{ detail.row.prompt_tokens }} / {{ detail.row.completion_tokens }})</el-descriptions-item>
        <el-descriptions-item label="Cost ($)">{{ money(detail.row.cost) }}</el-descriptions-item>
        <el-descriptions-item label="Latency">{{ detail.row.latency_ms }} ms</el-descriptions-item>
        <el-descriptions-item v-if="detail.row.error_code" label="Error">
          <pre class="error-detail">{{ detail.row.error_code }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-drawer>
  </el-card>
</template>

<style scoped>
.filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.log-table :deep(.el-table__row) {
  cursor: pointer;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.error-detail {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.5;
}
</style>
