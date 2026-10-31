<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import Terminal from './Terminal.vue'
import Toasts from './Toasts.vue'

const tabs = computed(() =>
  herd.tabs.map((tb, i) => {
    const t = herd.task(tb.task)
    const s = t?.steps.find((x) => x.id === tb.step)
    const kind = !s || s.state === 'done' || s.state === 'failed' ? 'off' : s.state === 'approval' ? 'approval' : 'live'
    return { i, id: tb.step, kind, label: `${t?.issue ? '#' + t.issue : 'LOCAL'} · ${s?.name ?? ''}`, project: t?.projectName ?? '', on: i === herd.active && !!herd.activeTab }
  }),
)
const step = computed(() => herd.selStep)
const t = computed(() => herd.selTask)
const agent = computed(() => (step.value ? herd.settings?.agents.find((a) => a.id === step.value!.agent) : undefined))
const model = (m: string) => (m ? m[0].toUpperCase() + m.slice(1) : '')
const chip = computed(() => {
  const s = step.value
  if (!s) return null
  if (s.state === 'approval') return { cls: 's-approval', text: 'Live · waiting for approval' }
  if (s.state === 'running' || s.state === 'idle') return { cls: 's-running', text: s.state === 'idle' ? 'Live · waiting for you' : 'Live' }
  if (s.state === 'failed') return { cls: 's-waiting', text: s.live ? 'Resumed · step failed' : 'Failed · resumes on click' }
  return { cls: 's-idle', text: s.live ? 'Resumed' : 'Resumed on click' }
})
const finished = computed(() => !!step.value && (step.value.state === 'done' || step.value.state === 'failed'))
</script>

<template>
  <section>
    <div class="tabs" role="tablist">
      <div v-for="tb in tabs" :key="tb.id" class="tab" :class="{ on: tb.on }">
        <button class="open" role="tab" :aria-selected="tb.on" @click="herd.focusTab(tb.i)">
          <span :class="['dot', `k-${tb.kind}`]"></span>
          <span>{{ tb.label }}</span>
          <span class="proj">{{ tb.project }}</span>
        </button>
        <button class="x" aria-label="Close tab" @click="herd.closeTab(tb.i)"><Icon name="close" :size="11" :stroke="2.4" /></button>
      </div>
      <span class="grow"></span>
    </div>

    <template v-if="step && t">
      <div class="toolbar">
        <span v-if="chip" :class="['chip', 'badge', chip.cls]"><span class="dot"></span>{{ chip.text }}</span>
        <span class="muted small">{{ agent?.name ?? step.agent }} agent · {{ model(step.model) }}</span>
        <span v-if="step.guidance" class="guide" :title="step.guidance">“{{ step.guidance }}”</span>
        <span class="grow"></span>
        <span class="session">{{ finished ? 'claude --resume ' : 'session ' }}{{ step.session.slice(0, 8) }}</span>
      </div>
      <Terminal :key="step.id" :step-id="step.id" />
    </template>
    <div v-else-if="t" class="empty">
      <span class="ico"><Icon name="terminal" :size="26" :stroke="1.6" /></span>
      <span class="title">{{ t.steps.length ? 'No step open for this task' : 'No session yet' }}</span>
      <span class="text">
        <template v-if="t.steps.length">Pick a step on the right to open it in a tab.</template>
        <template v-else-if="t.issue">
          Press {{ t.next.options[0]?.label ?? 'Plan' }} in the left panel: Claude reads issue #{{ t.issue }}{{ t.description ? ' and your description' : '' }} in {{ t.projectName }}.
        </template>
        <template v-else>Press {{ t.next.options[0]?.label ?? 'Plan' }} in the left panel: Claude works from the description only.</template>
      </span>
    </div>
    <div v-else class="empty">
      <span class="ico"><Icon name="terminal" :size="26" :stroke="1.6" /></span>
      <span class="title">No task selected</span>
      <span class="text">Create one with New task, or pick a task on the left.</span>
    </div>

    <Toasts />
  </section>
</template>

<style scoped>
section {
  flex: 999 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg);
  position: relative;
}
.tabs {
  display: flex;
  align-items: stretch;
  flex: none;
  height: 34px;
  background: var(--chrome);
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
  overflow-y: hidden;
}
.tabs::-webkit-scrollbar {
  height: 0;
}
.tab {
  flex: none;
  display: flex;
  align-items: center;
  border-right: 1px solid var(--border);
  background: var(--chrome);
  color: var(--muted);
}
.tab.on {
  background: var(--bg);
  color: var(--text);
  margin-bottom: -1px;
}
.open {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 100%;
  padding: 0 4px 0 12px;
  border: 0;
  background: transparent;
  color: inherit;
  white-space: nowrap;
  font-size: 13px;
}
.proj {
  font-size: 11.5px;
  color: var(--muted);
}
.dot {
  width: 6px;
  height: 6px;
}
.k-live {
  background: var(--running-dot);
}
.k-approval {
  background: var(--approval-dot);
}
.k-off {
  background: var(--faint);
}
.x {
  width: 20px;
  height: 20px;
  margin-right: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--muted);
}
.x:hover {
  background: var(--hover);
  color: var(--text);
}
.grow {
  flex: 1;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
  min-height: 32px;
  padding: 0 12px;
  border-bottom: 1px solid var(--border);
  min-width: 0;
}
.chip {
  font-size: 11.5px;
  align-self: center;
}
.chip .dot {
  background: var(--dot);
}
.small {
  font-size: 12px;
  white-space: nowrap;
}
.guide {
  font-size: 12px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}
.session {
  font-family: var(--mono);
  font-size: 11.5px;
  color: var(--muted);
  white-space: nowrap;
}
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 48px;
  text-align: center;
}
.ico {
  color: var(--faint);
  display: flex;
}
.title {
  font-size: 14px;
}
.text {
  font-size: 12.5px;
  color: var(--muted);
  max-width: 380px;
  line-height: 1.5;
}
</style>
