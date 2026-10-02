<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { settingsApi } from '../api'

const form = reactive<Record<string, string>>({
  default_timeout_ms: '60000', log_retain_days: '30', model_pricing: '{}',
  pull_interval_min: '60', pull_default_priority: '0', pull_default_weight: '1',
  model_pull_last: '',
})
const loading = ref(false)
const saving = ref(false)

interface SettingDef {
  key: string
  label: string
  type: 'number' | 'pricing'
  min?: number
  max?: number
  step?: number
  help: string
}

const defs: SettingDef[] = [
  { key: 'default_timeout_ms', label: 'Default timeout (ms)', type: 'number', min: 1000, step: 5000, help: 'Timeout used when a channel has no specific value' },
  { key: 'log_retain_days', label: 'Log retention (days)', type: 'number', min: 1, max: 3650, step: 1, help: 'Logs older than this are purged at startup' },
  { key: 'pull_interval_min', label: 'Auto-pull interval (min)', type: 'number', min: 0, step: 10, help: '0 = disabled. Runs immediately at startup, then on interval' },
  { key: 'pull_default_priority', label: 'Default pull priority', type: 'number', min: -10, max: 10, step: 1, help: 'Priority of auto-created routes (lower = preferred)' },
  { key: 'pull_default_weight', label: 'Default pull weight', type: 'number', min: 1, step: 1, help: 'Weight of auto-created routes' },
  { key: 'model_pricing', label: 'Model pricing', type: 'pricing', help: 'Fallback price per model, USD per 1M tokens (JSON). Routes with custom pricing override this.' },
]

const lastRun = computed(() => {
  const raw = form.model_pull_last
  if (!raw) return null
  try { return JSON.parse(raw) } catch { return null }
})

function pricingPreview(): string {
  try {
    const obj = JSON.parse(form.model_pricing || '{}')
    const keys = Object.keys(obj)
    if (!keys.length) return '(not configured)'
    return `${keys.length} model(s): ` + keys.slice(0, 3).join(', ') + (keys.length > 3 ? ' …' : '')
  } catch {
    return '(invalid JSON)'
  }
}

const pricingDialog = reactive({ visible: false, text: '' })

function openPricingEdit() {
  pricingDialog.text = form.model_pricing || '{}'
  pricingDialog.visible = true
}

function savePricing() {
  try {
    const parsed = JSON.parse(pricingDialog.text || '{}')
    if (typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('must be a JSON object')
    form.model_pricing = JSON.stringify(parsed)
    pricingDialog.visible = false
  } catch (e: any) {
    ElMessage.error('model_pricing is not valid JSON: ' + e?.message)
  }
}

async function load() {
  loading.value = true
  try {
    const items = await settingsApi.get()
    for (const it of items) form[it.key] = it.value
  } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    await settingsApi.update([
      { key: 'default_timeout_ms', value: String(Number(form.default_timeout_ms) || 60000) },
      { key: 'log_retain_days', value: String(Number(form.log_retain_days) || 30) },
      { key: 'model_pricing', value: form.model_pricing },
      { key: 'pull_interval_min', value: String(Math.max(0, Number(form.pull_interval_min) || 0)) },
      { key: 'pull_default_priority', value: String(Number(form.pull_default_priority) || 0) },
      { key: 'pull_default_weight', value: String(Math.max(1, Number(form.pull_default_weight) || 1)) },
    ])
    ElMessage.success('Saved')
    await load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="head">
        <span>Settings</span>
        <el-button type="primary" size="small" :loading="saving" @click="save">Save</el-button>
      </div>
    </template>

    <el-table v-loading="loading" :data="defs" size="small" border>
      <el-table-column prop="label" label="Setting" width="220" />
      <el-table-column prop="key" label="Key" width="190">
        <template #default="{ row }"><code class="key">{{ row.key }}</code></template>
      </el-table-column>
      <el-table-column label="Value" min-width="260">
        <template #default="{ row }">
          <el-input-number
            v-if="row.type === 'number'"
            :model-value="Number(form[row.key])"
            :min="row.min" :max="row.max" :step="row.step ?? 1" style="width: 180px"
            @update:model-value="(v: number) => (form[row.key] = String(v))"
          />
          <div v-else-if="row.type === 'pricing'" class="pricing-cell">
            <span class="pricing-preview">{{ pricingPreview() }}</span>
            <el-button size="small" @click="openPricingEdit">Edit JSON</el-button>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="help" label="Description" min-width="240" />
    </el-table>

    <!-- Last auto-pull run (read-only) -->
    <div v-if="lastRun" class="mt">
      <el-descriptions :column="6" size="small" border>
        <el-descriptions-item label="Last run">{{ new Date(lastRun.at * 1000).toLocaleString() }}</el-descriptions-item>
        <el-descriptions-item label="Created">{{ lastRun.created }}</el-descriptions-item>
        <el-descriptions-item label="Removed">{{ lastRun.removed }}</el-descriptions-item>
        <el-descriptions-item label="Skipped">{{ lastRun.skipped }}</el-descriptions-item>
        <el-descriptions-item label="Failed channels">{{ lastRun.errored }}</el-descriptions-item>
        <el-descriptions-item label="Duration">{{ lastRun.cost_ms }}ms</el-descriptions-item>
      </el-descriptions>
    </div>
    <el-alert v-else class="mt" title="Auto-pull has not run yet (set the interval above to a value > 0 and save)" type="info" :closable="false" />

    <!-- Pricing editor -->
    <el-dialog v-model="pricingDialog.visible" title="Model Pricing (JSON)" width="560px">
      <el-input v-model="pricingDialog.text" type="textarea" :rows="12" class="code" />
      <div class="tip mt">Format: {"model": {"input": USD-per-1M-input-tokens, "output": USD-per-1M-output-tokens}}, e.g. {"deepseek-chat": {"input": 0.14, "output": 0.28}}</div>
      <template #footer>
        <el-button @click="pricingDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="savePricing">OK</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
.key { color: #909399; font-size: 12px; }
.pricing-cell { display: flex; align-items: center; gap: 10px; }
.pricing-preview { color: #606266; font-size: 13px; }
.mt { margin-top: 12px; }
.tip { font-size: 12px; color: #909399; line-height: 1.6; }
.code :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; }
</style>