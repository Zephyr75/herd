<script setup lang="ts">
// Files a step changed: its commit, or the uncommitted work while it is the
// task's latest step. A click opens the diff pop-up on that file.
import { herd } from '../lib/state'
import type { Changes } from '../lib/types'
import ChangedFiles from './ChangedFiles.vue'

defineProps<{ changes: Changes | null }>()

function pick(path: string) {
  if (herd.selStep) herd.openDiffPopup(herd.selStep.id, path)
}
</script>

<template>
  <p v-if="!changes" class="empty">Loading…</p>
  <p v-else-if="!changes.source && !changes.files.length" class="empty">This step did not change code: it only wrote its artifact.</p>
  <p v-else-if="!changes.files.length" class="empty">{{ changes.source === 'worktree' ? 'No uncommitted changes in the worktree.' : 'The commit changed no files.' }}</p>
  <ChangedFiles v-else :changes="changes" :task="herd.selTask?.id" @pick="pick" />
</template>

<style scoped>
.empty {
  margin: 0;
  color: var(--muted);
  font-size: 12.5px;
  line-height: 1.5;
}
</style>
