<script setup lang="ts">
// "#4356" in mono, or a LOCAL chip.
defineProps<{ issue: string; width?: string }>()
</script>

<template>
  <span v-if="issue" class="issue-ref" :style="width ? { width } : undefined">#{{ issue }}</span>
  <span v-else class="local-chip">LOCAL</span>
</template>
