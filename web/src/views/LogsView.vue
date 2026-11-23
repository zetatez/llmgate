<script setup lang="ts">
import RequestLogsPanel from '../components/RequestLogsPanel.vue'

defineProps<{ scope?: 'admin' | 'user' }>()
</script>

<template>
  <RequestLogsPanel :scope="scope" />
</template>
