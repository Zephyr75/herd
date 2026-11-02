<script setup lang="ts">
// The files a step changed, with a summary line: used by the Changes tab and
// the diff pop-up. A click picks a file.
import { computed } from 'vue'
import type { Changes } from '../lib/types'

const props = defineProps<{ changes: Changes; selected?: string }>()
defineEmits<{ pick: [path: string] }>()

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
  c.source === 'worktree' ? 'Uncommitted in the worktree' : c.via ? `Committed by ${c.via} · ${c.sha}` : `Commit ${c.sha}`
</script>

<template>
  <div class="sum">
    <span>{{ sourceLabel(changes) }}</span>
    <span class="muted">· {{ totals.n }} file{{ totals.n === 1 ? '' : 's' }}</span>
    <span class="grow"></span>
    <span class="c-add">+{{ totals.add }}</span>
    <span class="c-del">−{{ totals.del }}</span>
  </div>
  <div class="files">
    <button v-for="f in changes.files" :key="f.path" class="file" :class="{ on: f.path === selected }" title="Show the diff" @click="$emit('pick', f.path)">
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
