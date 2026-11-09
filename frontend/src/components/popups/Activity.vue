<script setup lang="ts">
// Opened from the status bar counts: every task that is working or needs
// you, grouped by what it is doing. A click opens the task.
import { computed, onBeforeUnmount, ref } from 'vue'
import { herd } from '../../lib/state'
import TaskRef from '../ui/TaskRef.vue'
import type { Task } from '../../lib/types'

const now = ref(Date.now())
const tick = setInterval(() => (now.value = Date.now()), 30_000)
onBeforeUnmount(() => clearInterval(tick))

const groups = computed(() => {
  const active = herd.tasks.filter((t) => !t.archived)
  const of = (k: string) => active.filter((t) => t.status.kind === k)
  return [
    { kind: 'approval', label: 'Needs approval', tasks: of('approval') },
    { kind: 'waiting', label: 'Waiting for input', tasks: [...of('waiting'), ...of('failed')] },
    { kind: 'running', label: 'Running', tasks: of('running') },
  ].filter((g) => g.tasks.length)
})

// The step a task is on, and for how long.
function current(t: Task) {
  const s = [...t.steps].reverse().find((x) => x.state !== 'done') ?? t.steps[t.steps.length - 1]
  const what = t.status.label.includes(' · ') ? t.status.label.slice(t.status.label.indexOf(' · ') + 3) : t.status.label
  let since = ''
  const start = s?.startedAt ? Date.parse(s.startedAt) : NaN
  if (s && s.state !== 'done' && !isNaN(start) && start > 0) {
    const m = Math.max(0, Math.round((now.value - start) / 60000))
    since = m < 60 ? `${m}m` : `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}`
  }
  return { what, since, step: s }
}

function open(t: Task) {
  herd.overlay = 'none'
  herd.pickTask(t.id)
}
</script>

<template>
  <button class="scrim" aria-label="Close activity" @click="herd.overlay = 'none'"></button>
  <div class="box popup" role="dialog" aria-label="Activity">
    <div class="head">
      <span class="title">Activity</span>
    </div>
    <div class="list">
      <p v-if="!groups.length" class="none">Nothing is running and nothing needs you.</p>
      <template v-for="g in groups" :key="g.kind">
        <span class="group"><span :class="['dot', 's-' + g.kind]"></span>{{ g.label }} <span class="muted">{{ g.tasks.length }}</span></span>
        <button v-for="t in g.tasks" :key="t.id" class="row" @click="open(t)">
          <TaskRef :issue="t.issue" width="46px" :task="t.id" />
          <span class="main">
            <span class="name">{{ t.title }}</span>
            <span class="sub">{{ current(t).what }} · {{ t.projectName }}</span>
          </span>
          <span class="meta">
            <span v-if="current(t).since" class="since" title="Time on this step">{{ current(t).since }}</span>
          </span>
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
  .scrim {
    z-index: 20;
  }
  .box {
    position: absolute;
    z-index: 21;
    left: 8px;
    bottom: 32px;
    width: 460px;
    max-width: calc(100% - 16px);
    max-height: 70%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 36px;
    flex: none;
    padding: 0 12px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    font-weight: 600;
  }
  .grow {
    flex: 1;
  }
  .small {
    font-size: 12px;
  }
  .list {
    overflow: auto;
    padding: 4px 4px 6px;
    display: flex;
    flex-direction: column;
  }
  .none {
    margin: 10px 8px;
    color: var(--muted);
    font-size: 12.5px;
  }
  .group {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 8px 8px 4px;
    font-size: 11.5px;
    color: var(--muted);
  }
  .group .dot {
    background: var(--dot);
    width: 6px;
    height: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    flex: none;
    padding: 6px 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    text-align: left;
  }
  .row:hover {
    background: var(--hover);
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name,
  .sub {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .name {
    font-size: 13px;
  }
  .sub {
    font-size: 12px;
    color: var(--muted);
  }
  .meta {
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--muted);
  }
</style>
