<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { portalApi, pricingApi } from '../api'
import type { ModelPrice } from '../api'

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

onMounted(load)
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
