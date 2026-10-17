<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import * as echarts from 'echarts'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi, pricingApi, logsApi } from '../api'
import type { ModelPrice, ModelUsage } from '../api'
import { PALETTE, AXIS_TOOLTIP, ITEM_TOOLTIP } from '../utils/chart'
import { usageVendorOf } from '../utils/vendor'

const props = defineProps<{ readonly?: boolean }>()

interface EditRow extends ModelPrice {
  dirty?: boolean
}

const isAdmin = computed(() => !props.readonly)
const tab = ref<'prices' | 'manage'>('prices')

const rows = ref<EditRow[]>([])
const draft = ref<EditRow[]>([])
const loading = ref(false)
const saving = ref(false)

const search = ref('')
const manageSearch = ref('')
const viewPage = ref(1)
const viewSize = ref(20)
const managePage = ref(1)
const manageSize = ref(20)

const newDialog = reactive({ visible: false, model: '', input: 0, output: 0, call: 0 })
const batchDialog = reactive({ visible: false, text: '', overwrite: true })

const viewList = computed(() => {
  const q = search.value.trim().toLowerCase()
  const out = q ? rows.value.filter((r) => r.model.toLowerCase().includes(q)) : rows.value
  return [...out].sort((a, b) => a.model.localeCompare(b.model))
})
const viewPaged = computed(() => {
  const start = (viewPage.value - 1) * viewSize.value
  return viewList.value.slice(start, start + viewSize.value)
})

const manageList = computed(() => {
  const q = manageSearch.value.trim().toLowerCase()
  const out = q ? draft.value.filter((r) => r.model.toLowerCase().includes(q)) : draft.value
  return [...out].sort((a, b) => a.model.localeCompare(b.model))
})
const managePaged = computed(() => {
  const start = (managePage.value - 1) * manageSize.value
  return manageList.value.slice(start, start + manageSize.value)
})
const dirtyRows = computed(() => draft.value.filter((r) => r.dirty))

const totalModels = computed(() => rows.value.length)

function fmtPrice(v: number): string {
  if (!v) return '$0'
  return '$' + (+v).toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
}

// 单次调用成本：标准请求 1K 输入 + 1K 输出 + 每次附加费
function callCost(r: Pick<ModelPrice, 'input' | 'output' | 'call'>): number {
  return (r.input || 0) * 0.001 + (r.output || 0) * 0.001 + (r.call || 0)
}

// $100 可调用次数
function callsPer100(r: Pick<ModelPrice, 'input' | 'output' | 'call'>): number {
  const c = callCost(r)
  return c > 0 ? 100 / c : 0
}

const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

// 价格公示页的条形图：按 $100 可调用次数排序（受搜索过滤影响）
const chartRows = computed(() =>
  viewList.value
    .map((r) => ({ model: r.model, calls: callsPer100(r), perCall: callCost(r) }))
    .filter((r) => r.calls > 0)
    .sort((a, b) => b.calls - a.calls),
)
// 固定窗口高度，窗口内通过 dataZoom 上下滑动查看全部模型
const CHART_VIEW_HEIGHT = 420
const VISIBLE_ROWS = 16

async function renderChart() {
  await nextTick()
  if (!chartEl.value || !chartRows.value.length) {
    chart?.dispose()
    chart = null
    return
  }
  const data = [...chartRows.value]
  const end = Math.min(VISIBLE_ROWS - 1, data.length - 1)
  chart?.dispose()
  chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: {
      ...AXIS_TOOLTIP,
      formatter: (params: any[]) => {
        const p = params[0]
        const row = data[p.dataIndex]
        return `${p.name}<br/>calls per $100: ${Math.round(row.calls).toLocaleString()}<br/>cost per call: $${row.perCall.toFixed(6)}`
      },
    },
    grid: { left: 8, right: 34, top: 8, bottom: 8, containLabel: true },
    dataZoom: [
      {
        type: 'slider', yAxisIndex: 0, right: 4, width: 10,
        startValue: 0, endValue: end, zoomLock: true, brushSelect: false,
        showDetail: false, showDataShadow: false, handleSize: 0, moveHandleSize: 0,
        borderColor: 'transparent',
        backgroundColor: 'rgba(255,255,255,.05)',
        fillerColor: 'rgba(139,147,214,.55)',
        textStyle: { color: '#8b909c' },
      },
      { type: 'inside', yAxisIndex: 0, startValue: 0, endValue: end, zoomOnMouseWheel: false, moveOnMouseWheel: true },
    ],
    xAxis: { type: 'value', axisLabel: { color: '#8b909c' }, splitLine: { lineStyle: { color: '#23252c' } } },
    yAxis: { type: 'category', inverse: true, data: data.map((d) => d.model), axisTick: { show: false }, axisLine: { lineStyle: { color: '#2a2c33' } }, axisLabel: { color: '#8b909c', width: 230, overflow: 'truncate' } },
    series: [{
      type: 'bar',
      barMaxWidth: 16,
      data: data.map((d) => d.calls),
      itemStyle: {
        borderRadius: [0, 3, 3, 0],
        color: (p: any) => PALETTE[p.dataIndex % PALETTE.length],
      },
    }],
  })
  chart.resize()
}

function onResize() {
  chart?.resize()
  vendorChart?.resize()
}

watch([chartRows, tab], () => renderChart())

// 实际使用占比：最近 30 天按厂商聚合 token
const usageModels = ref<ModelUsage[]>([])
const usageDays = 30
const vendorEl = ref<HTMLDivElement>()
let vendorChart: echarts.ECharts | null = null

const vendorUsage = computed(() => {
  const m = new Map<string, number>()
  for (const r of usageModels.value) {
    const v = usageVendorOf(r.model)
    m.set(v, (m.get(v) || 0) + (r.total_tokens || 0))
  }
  const arr = [...m.entries()].filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1])
  const top = arr.slice(0, 8)
  const rest = arr.slice(8).reduce((s, [, v]) => s + v, 0)
  if (rest > 0) top.push(['Other', rest])
  return top.map(([name, value]) => ({ name, value }))
})
// 模型占比
const modelUsageShare = computed(() => {
  const arr = [...usageModels.value].filter((m) => (m.total_tokens || 0) > 0).sort((a, b) => b.total_tokens - a.total_tokens)
  const top = arr.slice(0, 8).map((m) => ({ name: m.model, value: m.total_tokens }))
  const rest = arr.slice(8).reduce((s, m) => s + m.total_tokens, 0)
  if (rest > 0) top.push({ name: 'Other', value: rest })
  return top
})

// 实际使用占比切换：按模型 / 按厂商
const usageMode = ref<'model' | 'vendor'>('model')
const usageShare = computed(() => (usageMode.value === 'model' ? modelUsageShare.value : vendorUsage.value))
const usageTotal = computed(() => usageShare.value.reduce((s, d) => s + d.value, 0) || 1)

function humanNum(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return +(n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return +(n / 1e3).toFixed(1) + 'k'
  return String(n)
}

async function loadUsage() {
  try {
    const d = isAdmin.value ? await logsApi.dashboard(usageDays) : await portalApi.dashboard(usageDays)
    usageModels.value = d.models || []
  } catch {
    usageModels.value = []
  }
}

async function renderVendorChart() {
  await nextTick()
  if (!vendorEl.value || !usageShare.value.length) {
    vendorChart?.dispose()
    vendorChart = null
    return
  }
  const total = usageTotal.value
  vendorChart?.dispose()
  vendorChart = echarts.init(vendorEl.value)
  vendorChart.setOption({
    tooltip: {
      ...ITEM_TOOLTIP,
      formatter: (p: any) => `${p.marker}${p.name}: ${humanNum(p.value)} tokens (${((p.value / total) * 100).toFixed(1)}%)`,
    },
    legend: {
      type: 'scroll', orient: 'vertical', right: 0, top: 'center',
      itemWidth: 9, itemHeight: 9, itemGap: 8,
      textStyle: { color: '#8b909c', fontSize: 11 },
      formatter: (name: string) => {
        const d = usageShare.value.find((v) => v.name === name)
        const raw = d ? (d.value / total) * 100 : 0
        const pct = raw > 0 && raw < 1 ? raw.toFixed(2) : raw.toFixed(1)
        const label = name.length > 24 ? name.slice(0, 23) + '…' : name
        return `${label}  ${pct}%`
      },
    },
    series: [{
      type: 'pie',
      radius: ['52%', '78%'],
      center: ['34%', '50%'],
      itemStyle: { borderColor: 'rgba(18,22,32,.9)', borderWidth: 2 },
      label: { show: false },
      labelLine: { show: false },
      color: PALETTE,
      data: usageShare.value,
    }],
  })
  vendorChart.resize()
}

watch([usageShare, usageMode], () => renderVendorChart())

function syncDraft() {
  draft.value = rows.value.map((r) => ({ ...r, dirty: false }))
  managePage.value = 1
}

function openManage() {
  syncDraft()
  tab.value = 'manage'
}

function markDirty(row: EditRow) {
  row.dirty = true
}

function discardChanges() {
  syncDraft()
  ElMessage.info('Changes discarded')
}

async function load() {
  loading.value = true
  try {
    const list = isAdmin.value ? await pricingApi.list() : await portalApi.pricing()
    rows.value = list.map((r) => ({ ...r }))
    syncDraft()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to load model prices')
  } finally {
    loading.value = false
  }
}

async function applyResult(list: ModelPrice[]) {
  rows.value = list.map((r) => ({ ...r }))
  syncDraft()
}

async function saveDirty() {
  const changed = dirtyRows.value
  if (!changed.length) return
  saving.value = true
  try {
    const list = await pricingApi.save(changed.map((r) => ({ model: r.model, input: r.input, output: r.output, call: r.call })))
    await applyResult(list)
    ElMessage.success(`Saved ${changed.length} model price(s)`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to save')
  } finally {
    saving.value = false
  }
}

function openNew() {
  Object.assign(newDialog, { visible: true, model: '', input: 0, output: 0, call: 0 })
}

async function submitNew() {
  const model = newDialog.model.trim()
  if (!model) { ElMessage.warning('Model name is required'); return }
  saving.value = true
  try {
    const list = await pricingApi.save([{ model, input: +newDialog.input || 0, output: +newDialog.output || 0, call: +newDialog.call || 0 }])
    await applyResult(list)
    newDialog.visible = false
    ElMessage.success('Model price added')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to save')
  } finally {
    saving.value = false
  }
}

async function removeRow(row: EditRow) {
  try {
    await ElMessageBox.confirm(`Delete the price for "${row.model}"?`, 'Delete price', { type: 'warning' })
  } catch { return }
  saving.value = true
  try {
    const list = await pricingApi.remove([row.model])
    await applyResult(list)
    ElMessage.success('Deleted')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to delete')
  } finally {
    saving.value = false
  }
}

interface BatchRow { model: string; input: number; output: number; call: number }

// 支持 JSON（对象或数组）以及 CSV/TSV：model,input,output[,call]
function parseBatch(text: string): BatchRow[] {
  const raw = text.trim()
  if (!raw) throw new Error('nothing to import')
  if (raw.startsWith('{') || raw.startsWith('[')) {
    const obj = JSON.parse(raw)
    if (Array.isArray(obj)) {
      return obj.map((r: any) => {
        const model = String(r.model ?? r.name ?? '').trim()
        if (!model) throw new Error('missing model in JSON array')
        return { model, input: +r.input || 0, output: +r.output || 0, call: +r.call || 0 }
      })
    }
    return Object.entries(obj).map(([model, v]: [string, any]) => {
      if (!model.trim()) throw new Error('missing model name')
      return { model: model.trim(), input: +v?.input || 0, output: +v?.output || 0, call: +v?.call || 0 }
    })
  }
  const out: BatchRow[] = []
  for (const line of raw.split(/\r?\n/)) {
    const l = line.trim()
    if (!l || l.startsWith('#')) continue
    const parts = l.split(/[,\t;]+/).map((s) => s.trim())
    if (parts[0].toLowerCase() === 'model') continue
    if (parts.length < 3) throw new Error(`invalid line: ${line}`)
    out.push({ model: parts[0], input: +parts[1] || 0, output: +parts[2] || 0, call: +(parts[3] || 0) })
  }
  if (!out.length) throw new Error('no rows parsed')
  return out
}

const batchPreview = computed(() => {
  const text = batchDialog.text.trim()
  if (!text) return { rows: [] as BatchRow[], added: 0, updated: 0, error: '' }
  try {
    const parsed = parseBatch(text)
    const existing = new Set(rows.value.map((r) => r.model))
    // 同名去重（后出现的覆盖先出现的），再统计新增/覆盖数量
    const byName = new Map<string, BatchRow>()
    for (const r of parsed) byName.set(r.model, r)
    const unique = [...byName.values()]
    const updated = unique.filter((r) => existing.has(r.model)).length
    return { rows: unique, added: unique.length - updated, updated, error: '' }
  } catch (e: any) {
    return { rows: [] as BatchRow[], added: 0, updated: 0, error: e?.message || 'invalid input' }
  }
})

const batchImportRows = computed(() => {
  if (!batchDialog.overwrite) {
    const existing = new Set(rows.value.map((r) => r.model))
    return batchPreview.value.rows.filter((r) => !existing.has(r.model))
  }
  return batchPreview.value.rows
})

async function submitBatch() {
  const parsed = batchImportRows.value
  if (!parsed.length) return
  saving.value = true
  try {
    const list = await pricingApi.save(parsed)
    await applyResult(list)
    batchDialog.visible = false
    batchDialog.text = ''
    ElMessage.success(`Imported ${parsed.length} model price(s)`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Failed to import')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await Promise.all([load(), loadUsage()])
  await nextTick()
  renderChart()
  renderVendorChart()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  chart?.dispose()
  vendorChart?.dispose()
})
</script>

<template>
  <div v-loading="loading">
    <div class="page-head">
      <div>
        <h2 class="page-title">Model pricing</h2>
        <p class="page-sub">
          USD per 1M tokens; per-call fee extra. Route prices take precedence.
        </p>
      </div>
    </div>

    <el-tabs v-if="isAdmin" v-model="tab" class="mt">
      <el-tab-pane label="Prices" name="prices" />
      <el-tab-pane label="Manage" name="manage" />
    </el-tabs>

    <!-- 公示视图 -->
    <template v-if="tab === 'prices' || !isAdmin">
      <div class="toolbar">
        <el-input v-model="search" placeholder="Search model" clearable size="small" class="search" @input="viewPage = 1" />
        <span class="hint">{{ totalModels }} model(s)</span>
        <el-button v-if="isAdmin" type="primary" size="small" class="ml-auto" @click="openManage">Manage</el-button>
      </div>

      <el-row :gutter="14" class="mb">
        <el-col :xs="24" :lg="14" class="col-gap">
          <el-card shadow="never" class="fill">
            <template #header>
              <div class="head">
                <span>Calls per $100</span>
                <span class="hint">1K input + 1K output per call</span>
              </div>
            </template>
            <div v-if="chartRows.length" ref="chartEl" :style="{ height: CHART_VIEW_HEIGHT + 'px' }"></div>
            <el-empty v-else description="No usable prices" :image-size="50" />
          </el-card>
        </el-col>
        <el-col :xs="24" :lg="10">
          <el-card shadow="never" class="fill">
            <template #header>
              <div class="head">
                <span>Usage share <span class="hint">last 30 days · tokens</span></span>
                <el-radio-group v-model="usageMode" size="small">
                  <el-radio-button value="model">Model</el-radio-button>
                  <el-radio-button value="vendor">Vendor</el-radio-button>
                </el-radio-group>
              </div>
            </template>
            <div v-if="usageShare.length" ref="vendorEl" :style="{ height: CHART_VIEW_HEIGHT + 'px' }"></div>
            <el-empty v-else description="No usage yet" :image-size="50" />
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never">
        <el-table :data="viewPaged" size="small" row-key="model">
          <el-table-column prop="model" label="Model" min-width="280" show-overflow-tooltip />
          <el-table-column label="Input ($/1M)" width="150" align="right">
            <template #default="{ row }"><span class="price">{{ fmtPrice(row.input) }}</span></template>
          </el-table-column>
          <el-table-column label="Output ($/1M)" width="150" align="right">
            <template #default="{ row }"><span class="price">{{ fmtPrice(row.output) }}</span></template>
          </el-table-column>
          <el-table-column label="Per-call ($)" width="130" align="right">
            <template #default="{ row }"><span class="price">{{ fmtPrice(row.call) }}</span></template>
          </el-table-column>
          <el-table-column label="Calls / $100" width="140" align="right" sortable :sort-by="(row: EditRow) => callsPer100(row)">
            <template #default="{ row }">
              <span class="price">{{ callsPer100(row) ? Math.round(callsPer100(row)).toLocaleString() : '—' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && !viewList.length" description="No model prices yet" />
        <div v-if="viewList.length > viewSize" class="pager">
          <el-pagination
            v-model:current-page="viewPage" v-model:page-size="viewSize"
            :page-sizes="[20, 50, 100]" :total="viewList.length"
            layout="total, sizes, prev, pager, next" size="small" background
          />
        </div>
      </el-card>
    </template>

    <!-- 管理视图 -->
    <template v-else>
      <div class="toolbar">
        <el-input v-model="manageSearch" placeholder="Search model" clearable size="small" class="search" @input="managePage = 1" />
        <el-button type="primary" size="small" @click="openNew">New price</el-button>
        <el-button size="small" @click="batchDialog.visible = true">Batch import</el-button>
        <span class="hint">{{ manageList.length }} model(s)</span>
      </div>

      <el-card shadow="never">
        <el-table :data="managePaged" size="small" row-key="model">
          <el-table-column prop="model" label="Model" min-width="260" show-overflow-tooltip />
          <el-table-column label="Input ($/1M)" width="180" align="right">
            <template #default="{ row }">
              <el-input-number v-model="row.input" :min="0" :precision="6" :step="0.01" :controls="false" size="small" class="price-input" @change="markDirty(row)" />
            </template>
          </el-table-column>
          <el-table-column label="Output ($/1M)" width="180" align="right">
            <template #default="{ row }">
              <el-input-number v-model="row.output" :min="0" :precision="6" :step="0.01" :controls="false" size="small" class="price-input" @change="markDirty(row)" />
            </template>
          </el-table-column>
          <el-table-column label="Per-call ($)" width="160" align="right">
            <template #default="{ row }">
              <el-input-number v-model="row.call" :min="0" :precision="6" :step="0.01" :controls="false" size="small" class="price-input" @change="markDirty(row)" />
            </template>
          </el-table-column>
          <el-table-column label="Actions" width="90" align="right">
            <template #default="{ row }">
              <el-button link type="danger" size="small" @click="removeRow(row)">delete</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && !manageList.length" description="No model prices yet" />
        <div v-if="manageList.length > manageSize" class="pager">
          <el-pagination
            v-model:current-page="managePage" v-model:page-size="manageSize"
            :page-sizes="[20, 50, 100]" :total="manageList.length"
            layout="total, sizes, prev, pager, next" size="small" background
          />
        </div>
      </el-card>

      <div v-if="dirtyRows.length" class="save-bar">
        <span>{{ dirtyRows.length }} unsaved change(s)</span>
        <div>
          <el-button size="small" @click="discardChanges">Discard</el-button>
          <el-button type="primary" size="small" :loading="saving" @click="saveDirty">Save changes</el-button>
        </div>
      </div>
    </template>

    <el-dialog v-model="newDialog.visible" title="New model price" width="480px">
      <el-form label-width="130px">
        <el-form-item label="Model" required>
          <el-input v-model="newDialog.model" placeholder="e.g. deepseek-chat" />
        </el-form-item>
        <el-form-item label="Input ($/1M)">
          <el-input-number v-model="newDialog.input" :min="0" :precision="6" :step="0.01" />
        </el-form-item>
        <el-form-item label="Output ($/1M)">
          <el-input-number v-model="newDialog.output" :min="0" :precision="6" :step="0.01" />
        </el-form-item>
        <el-form-item label="Per-call ($)">
          <el-input-number v-model="newDialog.call" :min="0" :precision="6" :step="0.01" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="newDialog.visible = false">Cancel</el-button>
        <el-button type="primary" :loading="saving" @click="submitNew">Save</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="batchDialog.visible" title="Batch import prices" width="720px">
      <el-input
        v-model="batchDialog.text" type="textarea" :autosize="{ minRows: 12, maxRows: 28 }"
        class="code" spellcheck="false"
        placeholder="JSON object: {&quot;model&quot;: {&quot;input&quot;: 0.14, &quot;output&quot;: 0.28, &quot;call&quot;: 0}}&#10;JSON array: [{&quot;model&quot;: &quot;model&quot;, &quot;input&quot;: 0.1, &quot;output&quot;: 0.2}]&#10;CSV / TSV: model,input,output[,call]"
      />
      <div class="batch-options">
        <el-switch v-model="batchDialog.overwrite" active-text="Overwrite existing models with the same name" />
      </div>
      <div class="batch-status">
        <span v-if="batchPreview.error" class="err">Parse error: {{ batchPreview.error }}</span>
        <span v-else-if="batchPreview.rows.length" class="ok">
          {{ batchPreview.rows.length }} row(s): {{ batchPreview.added }} new, {{ batchPreview.updated }} existing
          <template v-if="!batchDialog.overwrite && batchPreview.updated"> · {{ batchPreview.updated }} existing will be skipped</template>
        </span>
        <span v-else class="tip">Paste JSON or one row per line (model,input,output[,call]).</span>
      </div>
      <template #footer>
        <el-button @click="batchDialog.visible = false">Cancel</el-button>
        <el-button type="primary" :loading="saving" :disabled="!batchImportRows.length" @click="submitBatch">
          Import {{ batchImportRows.length || '' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.page-title { margin: 0; font-size: 20px; font-weight: 700; color: var(--ll-ink); }
.page-sub { margin: 4px 0 0; font-size: 12.5px; color: var(--ll-muted); }
.toolbar { display: flex; align-items: center; gap: 8px; margin: 4px 0 12px; }
.search { width: 260px; }
.hint { font-size: 12px; color: var(--ll-muted); }
.ml-auto { margin-left: auto; }
.mt { margin-top: 6px; }
.mb { margin-bottom: 12px; }
.fill { height: 100%; }
.head { display: flex; justify-content: space-between; align-items: center; }
@media (max-width: 1199px) {
  .col-gap { margin-bottom: 12px; }
}
.price-input { width: 140px; }
.price { font-variant-numeric: tabular-nums; color: var(--ll-ink); }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.save-bar {
  position: sticky; bottom: 0; z-index: 5;
  display: flex; align-items: center; justify-content: space-between;
  margin-top: 12px; padding: 10px 14px;
  background: rgba(22, 26, 36, .96);
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  color: var(--ll-ink); font-size: 13px;
  backdrop-filter: blur(6px);
}
.batch-options { margin-top: 10px; }
.batch-status { margin-top: 8px; font-size: 12px; }
.batch-status .err { color: #c9808a; }
.batch-status .ok { color: #6f9a86; }
.batch-status .tip { color: var(--ll-muted); }
</style>
