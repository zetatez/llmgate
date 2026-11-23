<script setup lang="ts">
defineProps<{
  items: Array<{ label: string; value: string; sub?: string; neg?: boolean }>
}>()
</script>

<template>
  <el-row :gutter="14">
    <el-col v-for="it in items" :key="it.label" :xs="12" :md="6">
      <el-card shadow="never" class="sum">
        <div class="sum-num" :class="{ neg: it.neg }">{{ it.value }}</div>
        <div class="sum-label">
          {{ it.label }}<span v-if="it.sub" class="sum-sub"> · {{ it.sub }}</span>
        </div>
      </el-card>
    </el-col>
  </el-row>
</template>

<style scoped>
.sum { margin-bottom: 14px; }
.sum-num { font-size: 22px; font-weight: 700; color: var(--ll-ink); }
.sum-label { font-size: 12px; color: var(--ll-muted); margin-top: 2px; }
.sum-sub { color: var(--ll-muted); }
.neg { color: #c9808a; }
</style>
