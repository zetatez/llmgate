<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { channelsApi, logsApi, portalApi, usersApi } from '../api'
import type { RequestLog, User } from '../api'

const props = defineProps<{
  scope?: 'admin' | 'user'
  compact?: boolean
  dense?: boolean
  fill?: boolean
  channelId?: number | null
}>()
const isUser = computed(() => props.scope === 'user')
const compact = computed(() => !!props.compact)
const dense = computed(() => !!props.dense)
const fill = computed(() => !!props.fill)

const list = ref<RequestLog[]>([])
const loading = ref(false)
const status = ref('')
const displayModel = ref('')
const range = ref<[Date, Date] | null>(null)
const channelFilter = ref<number | ''>('')
const userId = ref<number | ''>('')
const channels = ref<Array<{ id: number; name: string }>>([])
const users = ref<User[]>([])
const page = ref(1)
const size = ref(50)
const total = ref(0)
const clearing = ref(false)

function fmtTime(ts: number) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  // 窄栏里省略年份，仍保留日期与秒
  if (compact.value) return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function baseParams(): Record<string, string | number> {
  const params: Record<string, string | number> = {}
  if (status.value) params.status = status.value
  if (displayModel.value) params.display_model = displayModel.value
  if (channelFilter.value) params.channel_id = channelFilter.value
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
  channelFilter.value = ''
  userId.value = ''
  page.value = 1
  load()
}

// 窄栏模式：固定最近 100 条，不传筛选参数、不查总数
async function loadLatest() {
  loading.value = true
  try {
    const params: Record<string, string | number> = { limit: 100, offset: 0 }
    if (props.channelId) params.channel_id = props.channelId
    list.value = isUser.value ? await portalApi.logs(params as any) : await logsApi.list(params)
  } finally {
    loading.value = false
  }
}

// 整页模式：筛选 + 分页
async function loadPage() {
  loading.value = true
  try {
    const params = { ...baseParams(), limit: size.value, offset: (page.value - 1) * size.value }
    list.value = isUser.value ? await portalApi.logs(params as any) : await logsApi.list(params)
    total.value = isUser.value ? await portalApi.logsCount(params as any) : await logsApi.count(baseParams())
  } finally {
    loading.value = false
  }
}

async function load() {
  await (compact.value ? loadLatest() : loadPage())
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

// 与 ChannelsView 相同的清理动作：重置冷却由管理员接口提供，用户端无等价接口
async function clearCooldowns() {
  clearing.value = true
  try {
    await channelsApi.resetPenalties()
    ElMessage.success('Cooldowns cleared')
    await load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to clear cooldowns')
  } finally {
    clearing.value = false
  }
}

// Channels 页选中渠道时锁定过滤；清空选择（null）则回到全部渠道
watch(
  () => props.channelId,
  () => {
    if (compact.value) {
      load()
      return
    }
    const next = props.channelId ?? ''
    if (channelFilter.value === next) return
    channelFilter.value = next
    page.value = 1
    load()
  },
)

onMounted(async () => {
  // 窄栏模式没有筛选控件，无需额外加载渠道/用户列表
  if (compact.value) {
    await load()
    return
  }
  if (props.channelId) channelFilter.value = props.channelId
  await Promise.all([load(), loadFilters()])
})
</script>

<template>
  <el-card shadow="never" class="logs-panel" :class="{ compact, dense, fill }">
    <template #header>
      <div v-if="compact" class="panel-head">
        <div class="panel-title">
          Recent requests<span v-if="list.length" class="panel-count"> · latest {{ list.length }}</span>
        </div>
        <el-button
          v-if="!isUser"
          link
          type="primary"
          size="small"
          class="panel-action"
          :loading="clearing"
          :disabled="clearing"
          @click="clearCooldowns"
        >
          {{ clearing ? 'Clearing…' : 'Clear cooldowns' }}
        </el-button>
      </div>
      <div v-else class="filters">
        <el-input
          v-model="displayModel"
          placeholder="Model"
          clearable
          style="width: 160px"
          @keyup.enter="search"
        />
        <el-select v-model="status" placeholder="Status" clearable style="width: 110px">
          <el-option label="Success" value="success" />
          <el-option label="Failed" value="error" />
        </el-select>
        <el-select v-model="channelFilter" placeholder="Channel" clearable filterable style="width: 140px">
          <el-option v-for="c in channels" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <el-select
          v-if="!isUser"
          v-model="userId"
          placeholder="User"
          clearable
          filterable
          style="width: 140px"
        >
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
        <el-button v-if="!isUser" size="small" :loading="clearing" :disabled="clearing" @click="clearCooldowns">
          {{ clearing ? 'Clearing…' : 'Clear cooldowns' }}
        </el-button>
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
      :height="fill ? '100%' : undefined"
      :max-height="fill ? undefined : (compact ? '66vh' : '70vh')"
      :empty-text="compact ? 'No requests yet' : undefined"
      class="log-table"
    >
      <el-table-column label="Time" :width="compact ? 130 : 165" class-name="nowrap">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column v-if="compact" prop="display_model" label="Model" min-width="130" show-overflow-tooltip />
      <el-table-column v-else prop="display_model" label="Model" width="200" show-overflow-tooltip />
      <el-table-column v-if="!isUser" label="User" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.user_name || '—' }}</template>
      </el-table-column>
      <el-table-column label="Channel" :width="compact ? undefined : 120" :min-width="compact ? 120 : undefined" show-overflow-tooltip>
        <template #default="{ row }">{{ row.channel_name || `#${row.channel_id}` }}</template>
      </el-table-column>
      <el-table-column label="Upstream" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          {{ row.channel_name ? `${row.channel_name}/${row.upstream_model}` : row.upstream_model || '—' }}
        </template>
      </el-table-column>
      <el-table-column v-if="compact" label="Tokens" min-width="120" class-name="nowrap" show-overflow-tooltip>
        <template #default="{ row }">{{ row.total_tokens }} ({{ row.prompt_tokens }} / {{ row.completion_tokens }})</template>
      </el-table-column>
      <el-table-column v-else label="Tokens" width="90" class-name="nowrap" show-overflow-tooltip>
        <template #default="{ row }">{{ row.total_tokens }}</template>
      </el-table-column>
      <el-table-column prop="latency_ms" label="Latency (ms)" :width="compact ? 130 : 100" class-name="nowrap" />
      <el-table-column label="Status" :width="compact ? 78 : 90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error_code" label="Error" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.error_code" class="error-cell">{{ row.error_code }}</span>
          <span v-else class="dim">—</span>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="!compact" class="pager">
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
.panel-title {
  font-weight: 600;
  color: var(--ll-ink);
  margin-bottom: 8px;
}
/* 窄栏标题行：标题 + 行内动作，高度不随按钮变化 */
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.panel-head .panel-title {
  margin-bottom: 0;
}
.panel-action {
  flex-shrink: 0;
}
.panel-count {
  font-weight: 400;
  color: var(--ll-muted);
}
.filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.log-table :deep(.el-table .cell) {
  line-height: 1.4;
}
/* 每条日志只占一行：超长错误信息截断，完整内容由 tooltip 展示 */
.error-cell {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  word-break: normal;
  font-size: 12px;
  line-height: 1.4;
}
.dim {
  color: var(--ll-muted);
}
/* 密集模式（Channels 页右栏）：小字号 + 紧行距，尽量多显示字段 */
.logs-panel.dense :deep(.el-card__header) {
  padding: 6px 8px;
}
.logs-panel.dense .panel-title {
  font-size: 11px;
  margin-bottom: 0;
}
.logs-panel.dense :deep(.el-table) {
  font-size: 10.5px;
}
.logs-panel.dense :deep(.el-table .cell) {
  line-height: 1.25;
  padding: 0 4px;
}
.logs-panel.dense :deep(.el-table .el-table__cell) {
  padding: 2px 4px;
}
.logs-panel.dense :deep(.el-tag) {
  font-size: 9px;
  height: 16px;
  padding: 0 3px;
}
.logs-panel.dense .error-cell {
  font-size: 10.5px;
  line-height: 1.25;
}
/* 填充模式：卡片占满外部固定高度，表格撑满剩余空间并内部滚动 */
.logs-panel.fill {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.logs-panel.fill :deep(.el-card__body) {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 6px 8px;
}
.logs-panel.fill :deep(.el-table) {
  flex: 1 1 auto;
  min-height: 0;
}
</style>
