<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { settingsApi, type SettingItem } from '../api'

const form = reactive<Record<string, string>>({})
const rows = ref<SettingItem[]>([])
const loading = ref(false)
const saving = ref(false)
const active = ref('')

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

// key -> 友好标题
const titles: Record<string, string> = {
  default_timeout_ms: 'Default timeout',
  log_retain_days: 'Log retention',
  model_pricing: 'Model pricing',
  pull_interval_min: 'Pull interval',
  pull_default_priority: 'New route priority',
  pull_default_weight: 'New route weight',
  gateway_prefix: 'Gateway prefix',
  circuit_fail_threshold: 'Failure threshold',
  circuit_fail_window_s: 'Failure window',
  circuit_quota_cooldown_s: 'Quota cooldown',
  circuit_probe_backoff_s: 'Probe backoff',
  rate_limit_qps: 'Requests per second',
  rate_limit_concurrency: 'Concurrent requests',
}
function title(key: string): string {
  return titles[key] || key
}

// 分类 -> 图标 / 副标题
const sectionIcon: Record<string, string> = {
  Basic: 'Setting',
  Runtime: 'Monitor',
  Failover: 'WarningFilled',
  'Rate limit': 'Timer',
  'Auto-pull': 'Refresh',
  Logging: 'Document',
  Pricing: 'Coin',
}
const sectionDesc: Record<string, string> = {
  Basic: 'General platform defaults applied when a channel has no override.',
  Runtime: 'Read-only runtime configuration (set via environment).',
  Failover: 'Circuit-breaker thresholds, cooldowns and recovery probing.',
  'Rate limit': 'Per-user throttling applied at the gateway. 0 disables a limit.',
  'Auto-pull': 'Automatic discovery and sync of upstream models.',
  Logging: 'Request log retention performed on startup.',
  Pricing: 'Cost estimation used for usage statistics and quotas.',
}

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

const current = computed(() => groups.value.find((g) => g.classification === active.value) || groups.value[0])

function isEditable(row: SettingItem): boolean {
  return !!meta[row.key]
}

function pricingPreview(): string {
  try {
    const obj = JSON.parse(form.model_pricing || '{}')
    const keys = Object.keys(obj)
    if (!keys.length) return '(not configured)'
    return `${keys.length} model(s)`
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
    if (!active.value && groups.value.length) active.value = groups.value[0].classification
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
  <div class="settings" v-loading="loading">
    <aside class="nav">
      <div class="nav-title">Settings</div>
      <div
        v-for="g in groups"
        :key="g.classification"
        class="nav-item"
        :class="{ active: g.classification === current?.classification }"
        @click="active = g.classification"
      >
        <el-icon :size="15"><component :is="sectionIcon[g.classification] || 'Setting'" /></el-icon>
        <span>{{ g.classification }}</span>
      </div>
    </aside>

    <section class="content">
      <div class="page-head">
        <div>
          <h2 class="page-title">{{ current?.classification || 'Settings' }}</h2>
          <p class="page-sub">{{ sectionDesc[current?.classification || ''] || 'Platform-level configuration.' }}</p>
        </div>
        <el-button type="primary" :loading="saving" @click="save">Save</el-button>
      </div>

      <el-card shadow="never" v-if="current">
        <div v-for="row in current.items" :key="row.key" class="row">
          <div class="row-info">
            <div class="row-title">
              {{ title(row.key) }}
              <el-tag v-if="!isEditable(row)" size="small" type="info" effect="plain" class="ro">read-only</el-tag>
            </div>
            <div class="row-desc">{{ row.description }}</div>
          </div>
          <div class="row-control">
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
            <code v-else class="val">{{ form[row.key] || '—' }}</code>
          </div>
        </div>
      </el-card>
    </section>

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
.settings {
  display: flex;
  gap: 24px;
  align-items: flex-start;
}
.nav {
  width: 200px;
  flex-shrink: 0;
  position: sticky;
  top: 16px;
}
.nav-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--ll-ink);
  margin-bottom: 10px;
  padding-left: 10px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 10px;
  border-radius: 8px;
  color: var(--ll-muted);
  font-size: 13px;
  cursor: pointer;
  margin-bottom: 2px;
  transition: background 0.15s, color 0.15s;
}
.nav-item:hover {
  background: var(--ll-hover);
  color: var(--ll-ink);
}
.nav-item.active {
  background: var(--ll-soft);
  color: var(--ll-ink);
  font-weight: 600;
}
.content {
  flex: 1;
  min-width: 0;
}
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}
.page-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: var(--ll-ink);
}
.page-sub {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--ll-muted);
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 14px 0;
  border-top: 1px solid var(--ll-border);
}
.row:first-of-type {
  border-top: none;
  padding-top: 2px;
}
.row-info {
  min-width: 0;
}
.row-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: var(--ll-ink);
}
.ro {
  transform: scale(0.85);
}
.row-desc {
  font-size: 12.5px;
  color: var(--ll-muted);
  margin-top: 3px;
  line-height: 1.5;
}
.row-control {
  flex-shrink: 0;
}
.val {
  font-size: 12px;
  color: var(--ll-muted);
  word-break: break-all;
  max-width: 340px;
}
.pricing-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}
.pricing-preview {
  color: var(--ll-muted);
  font-size: 13px;
}
.mt {
  margin-top: 12px;
}
.tip {
  font-size: 12px;
  color: var(--ll-muted);
  line-height: 1.6;
}
.code :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
}
</style>