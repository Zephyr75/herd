<script setup lang="ts">
// Files a step changed: its commit, or the uncommitted work while it is the
// task's latest step. A click opens the file in Zed.
import { computed } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import type { Changes } from '../lib/types'

const props = defineProps<{ changes: Changes | null }>()

const totals = computed(() => {
  const f = props.changes?.files ?? []
  return { n: f.length, add: f.reduce((s, x) => s + x.added, 0), del: f.reduce((s, x) => s + x.deleted, 0) }
})
const split = (p: string) => {
  const i = p.lastIndexOf('/')
  return { dir: i >= 0 ? p.slice(0, i + 1) : '', name: i >= 0 ? p.slice(i + 1) : p }
}
const statusCls = (s: string) => ({ A: 'c-add', '?': 'c-add', D: 'c-del', M: 'c-mod' })[s] ?? 'c-mod'
const statusTitle = (s: string) => ({ A: 'Added', '?': 'New, not committed yet', D: 'Deleted', M: 'Modified' })[s] ?? s

// A committed file opens as it was in that commit (a deleted one opens the
// commit's diff); an uncommitted one opens in the worktree.
function open(path: string, status: string) {
  const t = herd.selTask
  const sha = props.changes?.source === 'commit' ? props.changes.sha : ''
  if (!t || (status === 'D' && !sha)) return
  void herd.attempt('Could not open the file', () => api.OpenFileInZed(t.id, path, sha))
}
const fileTitle = (status: string) => {
  const sha = props.changes?.source === 'commit' ? props.changes.sha : ''
  if (!sha) return status === 'D' ? 'Deleted' : 'Open in Zed'
  return status === 'D' ? `Deleted in ${sha}: open the commit in Zed` : `Open in Zed as it was in ${sha}`
}
</script>

<template>
  <p v-if="!changes" class="empty">Loading…</p>
  <p v-else-if="!changes.source" class="empty">This step did not change code: it only wrote its artifact.</p>
  <p v-else-if="!changes.files.length" class="empty">{{ changes.source === 'worktree' ? 'No uncommitted changes in the worktree.' : 'The commit changed no files.' }}</p>
  <template v-else>
    <div class="sum">
      <span>{{ changes.source === 'worktree' ? 'Uncommitted in the worktree' : changes.via ? `Committed by ${changes.via} · ${changes.sha}` : `Commit ${changes.sha}` }}</span>
      <span class="muted">· {{ totals.n }} file{{ totals.n === 1 ? '' : 's' }}</span>
      <span class="grow"></span>
      <span class="c-add">+{{ totals.add }}</span>
      <span class="c-del">−{{ totals.del }}</span>
    </div>
    <div class="files">
      <button v-for="f in changes.files" :key="f.path" class="file" :disabled="f.status === 'D' && changes.source !== 'commit'" :title="fileTitle(f.status)" @click="open(f.path, f.status)">
        <span :class="['st', statusCls(f.status)]" :title="statusTitle(f.status)">{{ f.status === '?' ? 'U' : f.status }}</span>
        <span class="path"><span class="dir">{{ split(f.path).dir }}</span>{{ split(f.path).name }}</span>
        <span v-if="f.binary" class="muted small">binary</span>
        <template v-else>
          <span v-if="f.added" class="c-add small">+{{ f.added }}</span>
          <span v-if="f.deleted" class="c-del small">−{{ f.deleted }}</span>
        </template>
      </button>
    </div>
  </template>
</template>

<style scoped>
.empty {
  margin: 0;
  color: var(--muted);
  font-size: 12.5px;
  line-height: 1.5;
}
.sum {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  font-family: var(--mono);
  white-space: nowrap;
}
.sum > span:first-child {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.grow {
  flex: 1;
}
.files {
  display: flex;
  flex-direction: column;
  margin: 0 -8px;
}
.file {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 26px;
  padding: 0 8px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--text);
  text-align: left;
  font-size: 12.5px;
}
.file:hover:not(:disabled) {
  background: var(--hover);
}
.file:disabled {
  opacity: 1;
}
.st {
  flex: none;
  width: 12px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  text-align: center;
}
.path {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dir {
  color: var(--muted);
}
.small {
  flex: none;
  font-family: var(--mono);
  font-size: 11.5px;
}
.c-add {
  color: var(--done);
}
.c-del {
  color: var(--approval);
}
.c-mod {
  color: var(--waiting);
}
</style>
