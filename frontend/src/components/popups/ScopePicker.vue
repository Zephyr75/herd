<script setup lang="ts">
// Scope picker (⌘O, or a click on project, worktree or branch): the open
// tasks of one project, labelled by the clicked attribute.
import { computed, onMounted, ref, watch } from 'vue'
import { herd, type ScopeMode } from '../../lib/state'
import { isMac } from '../../lib/keymap'

const project = ref(herd.selTask?.project ?? herd.projects[0]?.id ?? '')
const filter = ref('')
const hi = ref(0)
const input = ref<HTMLInputElement>()
onMounted(() => input.value?.focus())

const modes: [ScopeMode, string][] = [['project', 'Project'], ['worktree', 'Worktree'], ['branch', 'Branch']]
const pname = computed(() => herd.project(project.value)?.name ?? project.value)
const rows = computed(() =>
  herd.tasks
    .filter((t) => t.project === project.value && !t.archived)
    .filter((t) => {
      const q = filter.value.toLowerCase().trim()
      return !q || `${t.title} ${t.slug} ${t.branch} ${t.worktreeRel}`.toLowerCase().includes(q)
    }),
)
watch([filter, project], () => (hi.value = Math.max(0, rows.value.findIndex((t) => t.id === herd.sel))), { immediate: true })
const placeholder = computed(() =>
  herd.scope === 'project' ? `Filter tasks in ${pname.value}…` : herd.scope === 'worktree' ? `Filter worktrees in ${pname.value}…` : `Filter agent branches in ${pname.value}…`,
)
const openCount = (id: string) => herd.tasks.filter((t) => t.project === id && !t.archived).length
const primary = (t: (typeof rows.value)[number]) =>
  herd.scope === 'project' ? `${t.issue ? '#' + t.issue : 'LOCAL'}  ${t.title}` : herd.scope === 'worktree' ? t.worktreeRel : t.branch

function key(e: KeyboardEvent) {
  const mod = isMac() ? e.metaKey : e.ctrlKey
  if (e.key === 'ArrowDown') hi.value = Math.min(rows.value.length - 1, hi.value + 1)
  else if (e.key === 'ArrowUp') hi.value = Math.max(0, hi.value - 1)
  else if (e.key === 'Tab') {
    const i = modes.findIndex((m) => m[0] === herd.scope)
    herd.scope = modes[(i + (e.shiftKey ? 2 : 1)) % 3][0]
  } else if (e.key === 'Enter' && rows.value[hi.value]) {
    herd.pickTask(rows.value[hi.value].id)
    if (mod) void herd.openWorktree()
  } else return
  e.preventDefault()
}
</script>

<template>
  <button class="scrim" aria-label="Close picker" @click="herd.overlay = 'none'"></button>
  <div class="box popup" :class="{ mac: herd.os === 'darwin' }" role="dialog" aria-label="Scope picker">
    <div class="modes">
      <div class="pill" role="tablist">
        <button v-for="[m, label] in modes" :key="m" role="tab" :aria-selected="herd.scope === m" :class="{ on: herd.scope === m }" @click="herd.scope = m">{{ label }}</button>
      </div>
    </div>
    <div v-if="herd.scope === 'project'" class="chips">
      <button v-for="p in herd.projects" :key="p.id" class="seg" :class="{ on: p.id === project }" @click="project = p.id">{{ p.name }} {{ openCount(p.id) }}</button>
    </div>
    <div class="filter">
      <label for="sq" class="sr-only">Filter</label>
      <input id="sq" ref="input" v-model="filter" :placeholder="placeholder" autocomplete="off" spellcheck="false" @keydown="key" />
    </div>
    <div class="rows">
      <button v-for="(t, i) in rows" :key="t.id" class="row" :class="{ hi: i === hi }" @click="herd.pickTask(t.id)" @mousemove="hi = i">
        <span class="dot" :style="{ background: `var(--${t.status.kind === 'failed' ? 'approval' : t.status.kind}-dot)` }"></span>
        <span class="primary" :class="{ mono: herd.scope !== 'project' }">{{ primary(t) }}</span>
        <span class="secondary">{{ herd.scope === 'project' ? t.branch : t.title }}</span>
        <span class="status">{{ t.status.short }}</span>
      </button>
      <span v-if="!rows.length" class="none">Nothing in this scope.</span>
    </div>
  </div>
</template>

<style scoped>
  .box {
    position: absolute;
    z-index: 21;
    top: 38px;
    left: 110px;
    width: 500px;
    max-width: calc(100% - 32px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .box.mac {
    left: 184px;
  }
  .modes {
    padding: 8px 8px 0;
  }
  .chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    padding: 8px 8px 0;
  }
  .filter {
    display: flex;
    align-items: center;
    margin: 8px;
    padding: 0 10px;
    height: 30px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
  }
  input {
    flex: 1;
    border: 0;
    outline: none;
    background: transparent;
    color: var(--text);
    font-size: 13px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    padding: 0 4px 6px;
    max-height: 360px;
    overflow: auto;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    flex: none;
    height: 32px;
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
  }
  .primary {
    flex: none;
    max-width: 62%;
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .primary.mono {
    font-size: 12.5px;
  }
  .secondary {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .status {
    font-size: 12px;
    color: var(--muted);
  }
  .none {
    padding: 12px;
    color: var(--muted);
  }
</style>
