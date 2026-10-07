<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { settingsApi, type SettingItem } from '../api'

const form = reactive<Record<string, string>>({})
const rows = ref<SettingItem[]>([])
const loading = ref(false)
const saving = ref(false)

// 可控设置各自目前的数字/JSON 渲染参数（分类与描述由后端目录下发，不改写这里）。
interface SettingMeta {
  type: 'number' | 'pricing'
  min?: number
  max?: number
  step?: number
}
const meta: Record<string, SettingMeta> = {
  default_timeout_ms: { type: 'number', min: 1000, step: 5000 },
  log_retain_days: { type: 'number', min: 1, max: 3650, step: 1 },
  pull_interval_min: { type: 'number', min: 0, step: 10 },
  pull_default_priority: { type: 'number', min: -10, max: 10, step: 1 },
  pull_default_weight: { type: 'number', min: 1, step: 1 },
  model_pricing: { type: 'pricing' },
}

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
    rows.value = await settingsApi.get()
    for (const it of rows.value) form[it.key] = it.val
  } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    // 仅提交可改设置（有 meta 渲染类型者）；只读项跳过
    const payload = rows.value
      .filter(r => meta[r.key])
      .map(r => ({ key: r.key, val: String(form[r.key]) }))
    await settingsApi.update(payload)
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

    <el-table v-loading="loading" :data="rows" size="small" border>
      <el-table-column prop="classification" label="Classification" width="120" />
      <el-table-column prop="key" label="Key" width="190">
        <template #default="{ row }"><code class="key">{{ row.key }}</code></template>
      </el-table-column>
      <el-table-column label="Value" min-width="260">
        <template #default="{ row }">
          <el-input-number
            v-if="meta[row.key]?.type === 'number'"
            :model-value="Number(form[row.key])"
            :min="meta[row.key].min" :max="meta[row.key].max" :step="meta[row.key].step ?? 1" style="width: 180px"
            @update:model-value="(v: number) => (form[row.key] = String(v))"
          />
          <div v-else-if="meta[row.key]?.type === 'pricing'" class="pricing-cell">
            <span class="pricing-preview">{{ pricingPreview() }}</span>
            <el-button size="small" @click="openPricingEdit">Edit JSON</el-button>
          </div>
          <code v-else class="key">{{ form[row.key] }}</code>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="Description" min-width="280" />
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