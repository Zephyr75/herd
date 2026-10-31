<script setup lang="ts">
import { computed } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import TaskRef from './ui/TaskRef.vue'
import Preview from './Preview.vue'
import Splitter from './ui/Splitter.vue'
import type { Step } from '../lib/types'

const t = computed(() => herd.selTask)

function meta(s: Step): { text: string; cls: string } {
  if (s.state === 'running') return { text: 'running', cls: 'c-running' }
  if (s.state === 'approval') return { text: 'needs approval', cls: 'c-approval' }
  if (s.state === 'idle') return { text: 'waiting for you', cls: 'c-waiting' }
  if (s.state === 'failed') return { text: 'failed', cls: 'c-approval' }
  if (s.verdict) return s.verdict === 'approve' ? { text: 'approved', cls: 'c-done' } : { text: 'changes requested', cls: 'c-waiting' }
  const task = t.value
  if (task && s.state === 'done' && task.pr && herd.settings?.agents.find((a) => a.id === s.agent)?.push) return { text: 'PR #' + task.pr, cls: '' }
  return { text: '', cls: '' }
}
const dotCls = (s: Step) => (s.state === 'done' ? 'd-done' : s.state === 'running' || s.state === 'idle' ? 'd-running' : s.state === 'failed' ? 'd-failed' : 'd-approval')

async function archive() {
  const task = t.value
  if (task) await herd.attempt('Could not archive', () => api.ArchiveTask(task.id))
}
</script>

<template>
  <aside :style="{ width: herd.rightWidth + 'px' }">
    <Splitter side="right" />
    <template v-if="t">
      <div class="head">
        <div class="title-row">
          <TaskRef :issue="t.issue" />
          <span class="title">{{ t.title }}</span>
          <button v-if="!t.archived" class="icon-btn" aria-label="Archive task" title="Archive task" @click="archive"><Icon name="archive" /></button>
        </div>
        <span class="muted small">{{ t.source }}{{ t.pr ? ` · PR #${t.pr}` : '' }}</span>
        <p v-if="t.description" class="desc">{{ t.description }}</p>
      </div>

      <div class="steps-head">
        <span class="muted small">Steps</span>
        <span class="grow"></span>
        <button v-if="!t.worktreeRemoved" class="zed" @click="herd.openWorktree()"><Icon name="external" />Open worktree in Zed</button>
      </div>

      <div class="timeline">
        <p v-if="!t.steps.length" class="none">No steps yet.</p>
        <div v-for="(s, i) in t.steps" :key="s.id" class="row" :class="{ on: herd.selStep?.id === s.id }">
          <span class="rail">
            <span :class="['sdot', dotCls(s)]"></span>
            <span v-if="i < t.steps.length - 1 || t.next.ghost" class="line"></span>
          </span>
          <button class="step" @click="herd.openStep(t.id, s.id)">
            <span class="name-row">
              <span class="name">{{ s.name }}</span>
              <span v-if="meta(s).text" :class="['meta', meta(s).cls]">{{ meta(s).text }}</span>
            </span>
            <span class="sub">{{ s.sha ? 'commit ' + s.sha : 'session ' + s.session.slice(0, 8) }}{{ s.auto ? ' · auto' : '' }}</span>
          </button>
          <button v-if="s.sha" class="diff" aria-label="Open this step's diff in Zed" @click="herd.openDiff(s)">Zed diff</button>
        </div>
        <div v-if="t.next.ghost" class="row ghost">
          <span class="rail"><span class="sdot pending"></span></span>
          <span class="step static">
            <span class="name">{{ t.next.ghost.name }}</span>
            <span class="small muted">Starts automatically when {{ t.next.ghost.after }} finishes</span>
          </span>
        </div>
      </div>

      <Preview />
    </template>
  </aside>
</template>

<style scoped>

  aside {
    position: relative;
    flex: none;
    background: var(--bg);
    border-left: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .head {
    padding: 10px 14px 12px;
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: none;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .title {
    flex: 1;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .icon-btn {
    flex: none;
    width: 24px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .icon-btn:hover {
    background: var(--hover);
    color: var(--text);
  }
  .small {
    font-size: 12px;
  }
  .desc {
    margin: 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    font-size: 12.5px;
    line-height: 1.5;
    max-height: 120px;
    overflow: auto;
    white-space: pre-wrap;
  }
  .steps-head {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 32px;
    flex: none;
    padding: 0 8px 0 14px;
    border-bottom: 1px solid var(--border);
  }
  .grow {
    flex: 1;
  }
  .zed {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 8px;
    border-radius: 4px;
    border: 0;
    background: transparent;
    color: var(--text);
    font-size: 12.5px;
  }
  .zed:hover {
    background: var(--hover);
  }
  .timeline {
    display: flex;
    flex-direction: column;
    padding: 6px 6px 2px;
    gap: 1px;
    flex: none;
    max-height: 40%;
    overflow: auto;
  }
  .none {
    margin: 8px;
    color: var(--muted);
    font-size: 12.5px;
  }
  .row {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 6px 8px 0;
    border-radius: 6px;
  }
  .row.on {
    background: var(--sel);
  }
  .row.ghost {
    padding-left: 10px;
  }
  .rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    align-self: stretch;
    width: 12px;
  }
  .sdot {
    flex: none;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    margin-top: 4px;
  }
  .d-done { background: var(--done-dot); }
  .d-running { background: var(--running-dot); }
  .d-approval,
  .d-failed { background: var(--approval-dot); }
  .pending {
    border: 1px dashed var(--muted);
  }
  .line {
    flex: 1;
    width: 1px;
    background: var(--border);
    margin-top: 4px;
  }
  .step {
    flex: 1;
    min-width: 0;
    text-align: left;
    background: transparent;
    border: 0;
    color: var(--text);
    padding: 2px 0 10px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 13px;
  }
  .step.static .name {
    color: var(--muted);
    font-style: italic;
  }
  .name-row {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .name {
    font-weight: 500;
  }
  .meta {
    font-size: 12px;
    color: var(--muted);
  }
  .c-running { color: var(--running); }
  .c-approval { color: var(--approval); }
  .c-waiting { color: var(--waiting); }
  .c-done { color: var(--done); }
  .sub {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--muted);
  }
  .diff {
    align-self: flex-start;
    height: 22px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
</style>
