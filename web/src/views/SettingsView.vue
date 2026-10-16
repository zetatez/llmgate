<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { settingsApi, type SettingItem } from '../api'

const form = reactive<Record<string, string>>({})
const rows = ref<SettingItem[]>([])
const loading = ref(false)
const saving = ref(false)

// 可控设置各自的渲染参数（分类与描述由后端目录下发）
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
  rate_limit_qps: { type: 'number', min: 0, step: 1 },
  rate_limit_concurrency: { type: 'number', min: 0, step: 1 },
  circuit_fail_threshold: { type: 'number', min: 1, step: 1 },
  circuit_fail_window_s: { type: 'number', min: 1, step: 5 },
  circuit_quota_cooldown_s: { type: 'number', min: 1, step: 5 },
  circuit_probe_backoff_s: { type: 'number', min: 1, step: 5 },
  model_pricing: { type: 'pricing' },
}

// 按 classification 分组（保持后端顺序）
const groups = computed(() => {
  const out: { classification: string; items: SettingItem[] }[] = []
  for (const it of rows.value) {
    let g = out.find((x) => x.classification === it.classification)
    if (!g) {
      g = { classification: it.classification, items: [] }
      out.push(g)
    }
    g.items.push(it)
  }
  return out
})

function isEditable(row: SettingItem): boolean {
  return !!meta[row.key]
}

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
    const payload = rows.value
      .filter((r) => meta[r.key])
      .map((r) => ({ key: r.key, val: String(form[r.key]) }))
    await settingsApi.update(payload)
    ElMessage.success('Saved')
    await load()
  } catch (e: any) { ElMessage.error(e?.response?.data?.error || 'Failed to save') } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading">
    <div class="page-head">
      <div>
        <div class="title">Settings</div>
        <div class="sub">Platform-level configuration. Controls marked read-only are managed by the system.</div>
      </div>
      <el-button type="primary" :loading="saving" @click="save">Save</el-button>
    </div>

    <el-card shadow="never" v-for="g in groups" :key="g.classification" class="grp">
      <template #header><span class="grp-title">{{ g.classification }}</span></template>
      <div v-for="row in g.items" :key="row.key" class="field">
        <div class="field-info">
          <div class="field-key">
            <code>{{ row.key }}</code>
            <el-tag v-if="!isEditable(row)" size="small" type="info" effect="plain" class="ro">read-only</el-tag>
          </div>
          <div class="field-desc">{{ row.description }}</div>
        </div>
        <div class="field-control">
          <el-input-number
            v-if="meta[row.key]?.type === 'number'"
            :model-value="Number(form[row.key])"
            :min="meta[row.key].min" :max="meta[row.key].max" :step="meta[row.key].step ?? 1"
            :disabled="!isEditable(row)"
            @update:model-value="(v: number) => (form[row.key] = String(v))"
          />
          <div v-else-if="meta[row.key]?.type === 'pricing'" class="pricing-cell">
            <span class="pricing-preview">{{ pricingPreview() }}</span>
            <el-button size="small" @click="openPricingEdit">Edit JSON</el-button>
          </div>
          <code v-else class="val">{{ form[row.key] }}</code>
        </div>
      </div>
    </el-card>

    <!-- Pricing editor -->
    <el-dialog v-model="pricingDialog.visible" title="Model Pricing (JSON)" width="560px">
      <el-input v-model="pricingDialog.text" type="textarea" :rows="12" class="code" />
      <div class="tip mt">Format: {"model": {"input": USD-per-1M-input-tokens, "output": USD-per-1M-output-tokens}}, e.g. {"deepseek-chat": {"input": 0.14, "output": 0.28}}</div>
      <template #footer>
        <el-button @click="pricingDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="savePricing">OK</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.title {
  font-size: 16px;
  font-weight: 700;
  color: #1f2430;
}
.sub {
  font-size: 12px;
  color: #8a90a0;
  margin-top: 2px;
}
.grp {
  margin-bottom: 14px;
}
.grp-title {
  font-size: 14px;
  font-weight: 600;
}
.field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 12px 2px;
  border-top: 1px solid #eef0f4;
}
.field:first-of-type {
  border-top: none;
}
.field-info {
  min-width: 0;
}
.field-key {
  display: flex;
  align-items: center;
  gap: 8px;
}
.field-key code {
  font-size: 13px;
  color: #3a3f4f;
  font-weight: 600;
}
.ro {
  transform: scale(0.85);
}
.field-desc {
  font-size: 12px;
  color: #8a90a0;
  margin-top: 3px;
}
.field-control {
  flex-shrink: 0;
}
.val {
  font-size: 12px;
  color: #6b7280;
  word-break: break-all;
  max-width: 360px;
}
.pricing-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}
.pricing-preview {
  color: #606266;
  font-size: 13px;
}
.mt {
  margin-top: 12px;
}
.tip {
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
}
.code :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
}
</style>