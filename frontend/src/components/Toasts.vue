<script setup lang="ts">
import { herd } from '../lib/state'
</script>

<template>
  <div v-if="herd.toasts.length" class="stack" aria-live="polite">
    <div v-for="t in herd.toasts" :key="t.id" class="toast popup" :class="{ error: t.error }">
      <span class="title">{{ t.title }}</span>
      <span v-if="t.body" class="body">{{ t.body }}</span>
    </div>
  </div>
</template>

<style scoped>
.stack {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 10;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 470px;
}
.toast {
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.title {
  font-size: 12px;
  color: var(--muted);
}
.error .title {
  color: var(--approval);
}
.body {
  font-family: var(--mono);
  font-size: 11.5px;
  color: var(--code-kw);
  white-space: pre-wrap;
  word-break: break-word;
}
.error .body {
  color: var(--text);
}
</style>
