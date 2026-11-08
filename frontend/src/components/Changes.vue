<script setup lang="ts">
// Files a step changed: its commit, or the uncommitted work while it is the
// task's latest step. A click opens the diff pop-up on that file.
import { herd } from '../lib/state'
import type { Changes } from '../lib/types'
import ChangedFiles from './ChangedFiles.vue'
import NotesBar from './NotesBar.vue'

defineProps<{ changes: Changes | null }>()

function pick(path: string) {
  if (herd.selStep) herd.openDiffPopup(herd.selStep.id, path)
}
</script>

<template>
  <p v-if="!changes" class="empty">Loading…</p>
  <p v-else-if="!changes.source" class="empty">This step did not change code: it only wrote its artifact.</p>
  <p v-else-if="!changes.files.length" class="empty">{{ changes.source === 'worktree' ? 'No uncommitted changes in the worktree.' : 'The commit changed no files.' }}</p>
  <template v-else>
    <NotesBar v-if="herd.selTask?.notes.length" class="notes" :task="herd.selTask" />
    <ChangedFiles :changes="changes" @pick="pick" />
    <p class="hint">Open a file and click a line number to comment on it.</p>
  </template>
</template>

<style scoped>
.notes {
  margin-bottom: 10px;
}
.hint {
  margin: 10px 0 0;
  color: var(--faint);
  font-size: 12px;
}
.empty {
  margin: 0;
  color: var(--muted);
  font-size: 12.5px;
  line-height: 1.5;
}
</style>
