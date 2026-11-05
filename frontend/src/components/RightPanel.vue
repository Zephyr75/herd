<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import TaskRef from './ui/TaskRef.vue'
import Preview from './Preview.vue'
import Splitter from './ui/Splitter.vue'
import { unsuccessful, type Step } from '../lib/types'

const t = computed(() => herd.selTask)
// Agents set to "Show a Diff button" (Implement and Test by default) get one.
const diffs = (s: Step) => !!herd.settings?.agents.find((a) => a.id === s.agent)?.showDiff
const openIssue = () => t.value && herd.attempt('Could not open the issue', () => api.OpenURL(`https://github.com/${t.value!.issueRepo || t.value!.repo}/issues/${t.value!.issue}`))

function meta(s: Step): { text: string; cls: string } {
  if (s.paused && s.state !== 'done' && s.state !== 'failed') return { text: s.manual ? 'manual' : 'paused', cls: s.manual ? 'c-waiting' : '' }
  if (s.state === 'running') return { text: s.external ? 'waiting for Copilot' : 'running', cls: 'c-running' }
  if (s.state === 'approval') return { text: 'needs approval', cls: 'c-waiting' }
  if (s.state === 'idle') return { text: 'waiting for you', cls: 'c-waiting' }
  if (s.state === 'failed') return { text: 'ended early', cls: 'c-approval' }
  if (unsuccessful(s)) return { text: 'failed', cls: 'c-approval' }
  const task = t.value
  if (task && s.state === 'done' && task.pr && herd.settings?.agents.find((a) => a.id === s.agent)?.push) return { text: 'PR #' + task.pr, cls: '' }
  return { text: '', cls: '' }
}
const dotCls = (s: Step) => (unsuccessful(s) ? 'd-failed' : s.state === 'done' ? 'd-done' : s.paused ? 'd-paused' : s.state === 'running' ? 'd-running' : s.state === 'failed' ? 'd-failed' : 'd-waiting')

// Rerun (set by herd): the latest step when it commits, and the cycle's
// Review once you changed the worktree by hand (Edit opens it in Zed).

// Drag the line above the preview to give it more or less room.
const timeline = ref<HTMLDivElement>()
function dragSplit(e: PointerEvent) {
  const el = timeline.value
  if (!el) return
  const startY = e.clientY
  const startH = el.offsetHeight
  const max = (el.parentElement?.clientHeight ?? 800) - 160
  const handle = e.currentTarget as HTMLElement
  try {
    handle.setPointerCapture(e.pointerId)
  } catch {
    /* synthetic event */
  }
  const move = (m: PointerEvent) => herd.setTimelineHeight(Math.min(max, Math.max(36, startH + m.clientY - startY)))
  const up = () => {
    handle.removeEventListener('pointermove', move)
    handle.removeEventListener('pointerup', up)
  }
  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', up)
}

async function openRunDir() {
  const task = t.value
  if (task) await herd.attempt('Could not open the run folder', () => api.OpenRunDir(task.id))
}

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
          <button v-if="t.issue && (t.issueRepo || t.repo)" class="act" :title="`Open issue #${t.issue} on GitHub`" @click="openIssue">GitHub</button>
          <button v-if="!t.archived && !t.worktreeRemoved" class="act" title="Open the task's worktree in Zed to edit it by hand" @click="herd.openWorktree()">Edit</button>
          <button v-if="!t.archived" class="icon-btn" aria-label="Archive task" title="Archive task" @click="archive"><Icon name="archive" :size="16" /></button>
        </div>
        <span class="muted small">{{ t.source }}{{ t.pr ? ` · PR #${t.pr}` : '' }}</span>
        <div v-if="t.sisters.length" class="sisters">
          <span class="muted small">Sister</span>
          <button v-for="x in t.sisters" :key="x.id" class="chip" title="Go to the sister task: the same work in another repository" @click="herd.pickTask(x.id)">{{ x.project }} →</button>
        </div>
        <p v-if="t.description" class="desc">{{ t.description }}</p>
      </div>

      <div class="steps-head">
        <span class="muted small">Steps</span>
        <span class="grow"></span>
        <button class="icon-btn run-dir" :title="`Open the folder with this task's markdown files (${t.runDir})`" aria-label="Open the run folder" @click="openRunDir"><Icon name="folder" :size="16" /></button>
      </div>

      <div ref="timeline" class="timeline" :class="{ sized: herd.timelineHeight > 0 }" :style="herd.timelineHeight > 0 ? { height: herd.timelineHeight + 'px' } : {}">
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
            <span class="sub">{{ s.external ? 'run by herd · PR #' + t.pr : s.sha ? 'commit ' + s.sha : 'session ' + s.session.slice(0, 8) }}{{ s.auto ? ' · auto' : '' }}</span>
          </button>
          <span class="acts">
            <button v-if="s.rerun" class="act" :title="`Run ${s.name} again in its own session, on the current worktree`" @click="herd.rerunStep(s)"><Icon name="play" :size="12" />Rerun</button>
            <button v-if="s.diff && diffs(s)" class="act" aria-label="Open this step's changes in Zed" title="Open this step's changes in Zed: the commit it belongs to, or the uncommitted work" @click="herd.openDiff(s)">Diff</button>
          </span>
        </div>
        <div v-if="t.next.ghost" class="row ghost">
          <span class="rail"><span class="sdot pending"></span></span>
          <span class="step static">
            <span class="name">{{ t.next.ghost.name }}</span>
            <span v-if="t.next.ghost.manual" class="small muted">Start it when you are done with {{ t.next.ghost.after }}</span>
            <span v-else class="small muted">Starts automatically if {{ t.next.ghost.after }} succeeds</span>
          </span>
          <span v-if="t.next.ghost.start" class="acts">
            <button class="act" @click="herd.run(t.next.ghost.start)"><Icon name="play" :size="12" />Start</button>
          </span>
        </div>
      </div>

      <div class="hsplit" role="separator" aria-orientation="horizontal" title="Drag to resize; double-click to reset" @pointerdown.prevent="dragSplit" @dblclick="herd.setTimelineHeight(0)"></div>
      <Preview />
    </template>
  </aside>
</template>

<style scoped>

  aside {
    position: relative;
    flex: 0 1 auto; /* narrow windows: shrink before the terminal is cut */
    min-width: 240px;
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
    width: 28px;
    height: 26px;
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
  .timeline {
    display: flex;
    flex-direction: column;
    padding: 6px 6px 2px;
    gap: 1px;
    flex: none;
    max-height: 40%;
    overflow: auto;
  }
  .timeline.sized {
    max-height: none;
  }
  .hsplit {
    flex: none;
    height: 7px;
    margin: 0 0 -4px;
    position: relative;
    z-index: 2;
    cursor: row-resize;
  }
  .hsplit:hover,
  .hsplit:active {
    background: linear-gradient(to bottom, transparent 2px, var(--accent) 2px, var(--accent) 4px, transparent 4px);
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
  .d-paused { background: var(--faint); }
  .d-running { background: var(--running-dot); }
  .d-waiting { background: var(--waiting-dot); } /* Claude waits for an answer */
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
  .acts {
    flex: none;
    display: flex;
    gap: 4px;
    align-self: flex-start;
  }
  .act {
    display: flex;
    align-items: center;
    gap: 5px;
    height: 22px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
  .act:hover {
    background: var(--hover);
  }
  .sisters {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px 6px;
  }
  .chip {
    height: 22px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
  .chip:hover {
    background: var(--hover);
  }
</style>
