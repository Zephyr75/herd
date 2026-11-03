<script setup lang="ts">
// The files a step changed, with a summary line: used by the Changes tab and
// the diff pop-up. A click picks a file. Files edited in other repositories
// come after the worktree's, under their repository's name.
import { computed } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import type { Changes, ExternalRepo, FileChange } from '../lib/types'

// task: shows "Open in Zed" on the other repositories.
const props = defineProps<{ changes: Changes; selected?: string; task?: string }>()
defineEmits<{ pick: [path: string] }>()

const groups = computed<{ repo: ExternalRepo | null; files: FileChange[] }[]>(() => [
  { repo: null, files: props.changes.files.filter((f) => !f.repo) },
  ...props.changes.repos.map((r) => ({ repo: r, files: props.changes.files.filter((f) => f.repo === r.root) })),
])
const shown = (f: FileChange) => (f.repo ? f.path.slice(f.repo.length + 1) : f.path)
const openRepo = (root: string) => props.task && herd.attempt('Could not open Zed', () => api.OpenRepoInZed(props.task!, root))

const totals = computed(() => {
  const f = props.changes.files
  return { n: f.length, add: f.reduce((s, x) => s + x.added, 0), del: f.reduce((s, x) => s + x.deleted, 0) }
})
</script>

<script lang="ts">
export const split = (p: string) => {
  const i = p.lastIndexOf('/')
  return { dir: i >= 0 ? p.slice(0, i + 1) : '', name: i >= 0 ? p.slice(i + 1) : p }
}
export const statusCls = (s: string) => ({ A: 'c-add', '?': 'c-add', D: 'c-del', M: 'c-mod' })[s] ?? 'c-mod'
export const statusTitle = (s: string) => ({ A: 'Added', '?': 'New, not committed yet', D: 'Deleted', M: 'Modified' })[s] ?? s
export const sourceLabel = (c: Changes) =>
  !c.source ? 'Changed outside the worktree' : c.source === 'worktree' ? 'Uncommitted in the worktree' : c.via ? `Committed by ${c.via} · ${c.sha}` : `Commit ${c.sha}`
</script>

<template>
  <div class="sum">
    <span>{{ sourceLabel(changes) }}</span>
    <span class="muted">· {{ totals.n }} file{{ totals.n === 1 ? '' : 's' }}</span>
    <span class="grow"></span>
    <span class="c-add">+{{ totals.add }}</span>
    <span class="c-del">−{{ totals.del }}</span>
  </div>
  <div v-for="g in groups.filter((x) => x.files.length)" :key="g.repo?.root ?? ''" class="files">
    <div v-if="g.repo" class="repo" :title="g.repo.root">
      <span class="grow">Outside the worktree: {{ g.repo.name }} <span class="muted">· uncommitted</span></span>
      <button v-if="task" class="open" @click="openRepo(g.repo.root)">Open in Zed</button>
    </div>
    <button v-for="f in g.files" :key="f.path" class="file" :class="{ on: f.path === selected }" title="Show the diff" @click="$emit('pick', f.path)">
      <span :class="['st', statusCls(f.status)]" :title="statusTitle(f.status)">{{ f.status === '?' ? 'U' : f.status }}</span>
      <span class="path"><span class="dir">{{ split(shown(f)).dir }}</span>{{ split(shown(f)).name }}</span>
      <span v-if="f.binary" class="muted small">binary</span>
      <template v-else>
        <span v-if="f.added" class="c-add small">+{{ f.added }}</span>
        <span v-if="f.deleted" class="c-del small">−{{ f.deleted }}</span>
      </template>
    </button>
  </div>
</template>

<style scoped>
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
.repo {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  padding: 4px 8px;
  font-size: 12px;
  color: var(--waiting);
}
.open {
  height: 22px;
  padding: 0 8px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
}
.open:hover {
  background: var(--hover);
}
.muted {
  color: var(--muted);
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
.file:hover {
  background: var(--hover);
}
.file.on {
  background: var(--sel);
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
