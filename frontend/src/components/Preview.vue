<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import ChangesView from './Changes.vue'
import PRTracker from './PRTracker.vue'
import ArtifactView from './ArtifactView.vue'
import TodoList from './TodoList.vue'
import type { Artifact, Changes } from '../lib/types'

const step = computed(() => herd.selStep)
const art = ref<Artifact | null>(null)

let seq = 0
watch(
  () => [step.value?.id, step.value?.state, herd.artifactRev] as const,
  ([id]) => {
    const mine = ++seq
    if (!id) {
      art.value = null
      return
    }
    api.ReadArtifact(id).then(
      (a) => {
        if (mine === seq) art.value = a
      },
      () => {
        if (mine === seq) art.value = null
      },
    )
  },
  { immediate: true },
)

// The step's changed files, refreshed as the task changes (debounced: hooks
// fire often while a step works).
const changes = ref<Changes | null>(null)
let cseq = 0
let ctimer: ReturnType<typeof setTimeout> | undefined
function loadChanges(id: string | undefined, delay: number) {
  clearTimeout(ctimer)
  const mine = ++cseq
  if (!id) {
    changes.value = null
    return
  }
  ctimer = setTimeout(() => {
    api.StepChanges(id).then(
      (c) => mine === cseq && (changes.value = c),
      () => mine === cseq && (changes.value = { source: '', sha: '', via: '', files: [] }),
    )
  }, delay)
}
watch(
  () => step.value?.id,
  (id) => {
    changes.value = null
    loadChanges(id, 0)
  },
  { immediate: true },
)
watch(
  () => [herd.selTask, step.value?.sha, step.value?.state] as const,
  () => loadChanges(step.value?.id, 800),
)

const file = computed(() => art.value?.file || step.value?.artifact || '')
// The Changes tab only shows when the step changed files, the PR tab when the
// task has a PR; the artifact's tab stands in otherwise.
const task = computed(() => herd.selTask)
const openTodos = computed(() => task.value?.todos.filter((x) => !x.done).length ?? 0)
const tab = computed(() =>
  herd.rightTab === 'todos' && task.value ? 'todos' : herd.rightTab === 'pr' && task.value?.pr ? 'pr' : step.value && changes.value?.files.length && herd.rightTab === 'changes' ? 'changes' : 'preview',
)
</script>

<template>
  <div class="wrap">
    <div class="bar">
      <div class="tab" role="tab" tabindex="0" :class="{ on: tab === 'preview' }" @click="herd.rightTab = 'preview'" @keydown.enter="herd.rightTab = 'preview'">
        <Icon name="file" />{{ file || 'Preview' }}
        <button v-if="step && art?.exists" class="big" aria-label="Open in a large pop-up" title="Open in a large pop-up" @click.stop="herd.openArtifactPopup(step.id)">
          <Icon name="maximize" :size="16" />
        </button>
      </div>
      <button v-if="step && changes?.files.length" class="tab" :class="{ on: tab === 'changes' }" @click="herd.rightTab = 'changes'">
        <Icon name="diff" />{{ step.external ? 'Full changes' : 'Changes' }}<span class="count">{{ changes.files.length }}</span>
      </button>
      <button v-if="task?.pr" class="tab" :class="{ on: tab === 'pr' }" @click="herd.rightTab = 'pr'"><Icon name="pr" />PR #{{ task.pr }}</button>
      <div v-if="task" class="tab" role="tab" tabindex="0" :class="{ on: tab === 'todos' }" @click="herd.rightTab = 'todos'" @keydown.enter="herd.rightTab = 'todos'">
        <Icon name="todo" />Todos<span v-if="openTodos" class="count">{{ openTodos }}</span>
        <button class="big" aria-label="Open every todo list in a pop-up" title="Open every todo list in a pop-up" @click.stop="herd.openOverlay('todos')">
          <Icon name="maximize" :size="16" />
        </button>
      </div>
      <span class="grow"></span>
    </div>
    <div class="content">
      <template v-if="tab === 'todos' && task">
        <div class="todo-head">
          <span class="todo-title">Todos for this task</span>
          <span class="muted small">{{ openTodos }} open · {{ task.todos.length - openTodos }} done</span>
          <span class="grow"></span>
          <label class="hide-done"><input v-model="herd.hideDone" type="checkbox" />Hide done</label>
        </div>
        <TodoList :todos="task.todos" :hide-done="herd.hideDone" @change="herd.setTodos(task, $event)" :run="herd.todoAgent()?.name ?? 'Implement'" :blocked="herd.todoBlocked(task)" @run="herd.runTodo(task, $event)" />
      </template>
      <PRTracker v-else-if="tab === 'pr' && task" :task="task" />
      <ChangesView v-else-if="tab === 'changes'" :changes="changes" />
      <p v-else-if="!step" class="empty">Open a step to preview the artifact it wrote.</p>
      <p v-else-if="!art || !art.exists" class="empty">{{ file || 'The artifact' }} has not been written yet. It appears here as soon as {{ step.external ? "Copilot's review arrives" : 'Claude writes it' }}.</p>
      <ArtifactView v-else :art="art" :task="step!.taskId" />
    </div>
  </div>
</template>

<style scoped>

  .wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border);
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: stretch;
    flex: none;
    height: 34px;
    background: var(--chrome);
    border-bottom: 1px solid var(--border);
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 0 14px;
    border: 0;
    border-right: 1px solid var(--border);
    background: var(--chrome);
    color: var(--muted);
    white-space: nowrap;
    font-size: 13px;
  }
  .tab.on {
    background: var(--bg);
    color: var(--text);
    margin-bottom: -1px;
  }
  .count {
    font-family: var(--mono);
    font-size: 11px;
    padding: 0 5px;
    border-radius: 8px;
    background: var(--surface);
    color: var(--muted);
  }
  .grow {
    flex: 1;
  }
  .tab {
    cursor: default;
  }
  .big {
    flex: none;
    width: 24px;
    height: 24px;
    margin: 0 -6px 0 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .big:hover {
    background: var(--hover);
    color: var(--text);
  }
  .content {
    flex: 1;
    min-height: 0;
    padding: 16px 20px 28px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .todo-head {
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 0 6px 8px;
    border-bottom: 1px solid var(--border);
  }
  .todo-title {
    font-weight: 600;
    font-size: 14px;
  }
  .small {
    font-size: 12px;
  }
  .hide-done {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--muted);
  }
  .empty {
    margin: 0;
    color: var(--muted);
    font-size: 12.5px;
    line-height: 1.5;
  }
</style>
