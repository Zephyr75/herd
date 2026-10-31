<script setup lang="ts">
// Go to task (⌘P): every word must match the title, issue number, project,
// description or LOCAL. Open tasks first, archived last.
import { computed, onMounted, ref, watch } from 'vue'
import { herd } from '../../lib/state'
import { isMac } from '../../lib/keymap'
import Icon from '../ui/Icon.vue'
import TaskRef from '../ui/TaskRef.vue'

const query = ref(herd.previewQuery)
const hi = ref(0)
const list = ref<HTMLDivElement>()
const input = ref<HTMLInputElement>()
onMounted(() => input.value?.focus())

const matches = computed(() => {
  const words = query.value.toLowerCase().trim().split(/\s+/).filter(Boolean)
  const out = herd.tasks.filter((t) => {
    const hay = `${t.title} ${t.issue} ${t.projectName} ${t.issue ? '' : 'local'} ${t.description} ${t.slug}`.toLowerCase()
    return words.every((w) => hay.includes(w))
  })
  return out.sort((a, b) => Number(a.archived) - Number(b.archived))
})
watch(query, () => (hi.value = 0))

function key(e: KeyboardEvent) {
  const mod = isMac() ? e.metaKey : e.ctrlKey
  const rows = matches.value
  if (e.key === 'ArrowDown') hi.value = Math.min(rows.length - 1, hi.value + 1)
  else if (e.key === 'ArrowUp') hi.value = Math.max(0, hi.value - 1)
  else if (e.key === 'Enter' && rows[hi.value]) {
    herd.pickTask(rows[hi.value].id)
    if (mod) void herd.openWorktree()
  } else return
  e.preventDefault()
  list.value?.querySelectorAll('button')[hi.value]?.scrollIntoView({ block: 'nearest' })
}
</script>

<template>
  <button class="scrim" aria-label="Close search" @click="herd.overlay = 'none'"></button>
  <div class="box popup" role="dialog" aria-label="Go to task">
    <div class="search">
      <span class="muted ico"><Icon name="search" :size="14" /></span>
      <label for="pq" class="sr-only">Search tasks</label>
      <input id="pq" ref="input" v-model="query" placeholder="Search tasks in all projects…" autocomplete="off" spellcheck="false" @keydown="key" />
      <span class="kbd">Esc</span>
    </div>
    <span class="heading">{{ query.trim() ? `${matches.length} task${matches.length === 1 ? '' : 's'} match` : 'Recent tasks · all projects' }}</span>
    <div ref="list" class="rows">
      <button v-for="(t, i) in matches" :key="t.id" class="row" :class="{ hi: i === hi }" @click="herd.pickTask(t.id)" @mousemove="hi = i">
        <TaskRef :issue="t.issue" width="46px" />
        <span class="title">{{ t.title }}</span>
        <span class="proj">{{ t.projectName }}</span>
        <span :class="['badge', `s-${t.status.kind}`]"><span class="dot"></span>{{ t.status.short }}</span>
      </button>
      <span v-if="!matches.length" class="none">No tasks match “{{ query }}”.</span>
    </div>
    <div class="foot">
      <span><span class="kbd">↑↓</span> move</span>
      <span><span class="kbd">⏎</span> open last step</span>
      <span><span class="kbd">{{ isMac() ? '⌘⏎' : 'Ctrl+⏎' }}</span> open worktree in Zed</span>
      <span class="grow"></span>
      <span>Archived tasks included</span>
    </div>
  </div>
</template>

<style scoped>
  .box {
    position: absolute;
    z-index: 21;
    top: 64px;
    left: 50%;
    transform: translateX(-50%);
    width: 620px;
    max-width: calc(100% - 32px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .search {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    height: 40px;
    border-bottom: 1px solid var(--border);
  }
  .ico {
    display: flex;
  }
  input {
    flex: 1;
    border: 0;
    outline: none;
    background: transparent;
    color: var(--text);
    font-size: 14px;
  }
  .heading {
    padding: 8px 12px 4px;
    font-size: 11.5px;
    color: var(--muted);
  }
  .rows {
    display: flex;
    flex-direction: column;
    padding: 0 4px 6px;
    max-height: 420px;
    overflow: auto;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    flex: none;
    height: 34px;
    padding: 0 10px;
    border-radius: 6px;
    border: 0;
    text-align: left;
    font-size: 13px;
    color: var(--text);
    background: transparent;
  }
  .row.hi {
    background: var(--sel);
    box-shadow: inset 2px 0 0 var(--focus);
  }
  .title {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .proj {
    font-size: 12px;
    color: var(--muted);
  }
  .badge {
    align-self: center;
    flex: none;
  }
  .badge .dot {
    width: 6px;
    height: 6px;
  }
  .none {
    padding: 14px 12px;
    color: var(--muted);
  }
  .foot {
    display: flex;
    gap: 14px;
    padding: 7px 12px;
    border-top: 1px solid var(--border);
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
  .grow {
    flex: 1;
  }
</style>
