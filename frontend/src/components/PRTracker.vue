<script setup lang="ts">
// The Copilot step's tab: where the PR is in CI, and whether Copilot has
// reviewed it. herd refreshes both every minute; the review itself is the
// step's artifact (COPILOT-n.md) in the Preview tab.
import { computed } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import { unsuccessful, type CICheck, type Step, type Task } from '../lib/types'
import Icon from './ui/Icon.vue'

const props = defineProps<{ task: Task; step: Step }>()

const rank: Record<string, number> = { fail: 0, cancel: 1, pending: 2, pass: 3, skipping: 4 }
// One row per workflow (Tests, Regtests, Lint…), worst check first.
const workflows = computed(() => {
  const by = new Map<string, CICheck[]>()
  for (const c of props.task.checks) by.set(c.workflow || c.name, [...(by.get(c.workflow || c.name) ?? []), c])
  return [...by.entries()]
    .map(([name, cs]) => {
      cs = [...cs].sort((a, b) => rank[a.bucket] - rank[b.bucket])
      const passed = cs.filter((c) => c.bucket === 'pass' || c.bucket === 'skipping').length
      return { name, cs, bucket: cs[0].bucket, passed, open: cs.filter((c) => c.bucket !== 'pass' && c.bucket !== 'skipping') }
    })
    .sort((a, b) => rank[a.bucket] - rank[b.bucket] || a.name.localeCompare(b.name))
})
const cls = (b: string) => (b === 'fail' || b === 'cancel' ? 'd-failed' : b === 'pending' ? 'd-running' : 'd-done')
const word = (b: string) => ({ fail: 'failed', cancel: 'cancelled', pending: 'running', pass: 'passed', skipping: 'skipped' })[b] ?? b

const waiting = computed(() => props.step.state === 'running')
const open = (link: string) => link && herd.attempt('Could not open the link', () => api.OpenURL(link))
const stop = () => herd.attempt('Could not stop waiting', () => api.MarkDone(props.step.id))
</script>

<template>
  <div class="tracker">
    <div class="head">
      <span class="title">PR #{{ task.pr }}</span>
      <span class="muted small">{{ task.branch }} → {{ task.baseBranch }}</span>
      <span class="grow"></span>
      <button v-if="task.prUrl" class="act" @click="open(task.prUrl)"><Icon name="external" :size="13" />Open on GitHub</button>
    </div>

    <section>
      <h3>Copilot review</h3>
      <div class="line">
        <span :class="['sdot', waiting ? 'd-running' : unsuccessful(step) ? 'd-failed' : step.verdict === 'skipped' ? 'd-off' : 'd-done']"></span>
        <span v-if="waiting">Requested. herd checks every minute and writes {{ step.artifact }} when it arrives.</span>
        <span v-else-if="unsuccessful(step)">Copilot left comments: read them in the Preview tab ({{ step.artifact }}, also in FEEDBACK.md).</span>
        <span v-else-if="step.verdict === 'skipped'">herd stopped waiting; no review arrived.</span>
        <span v-else>Reviewed: nothing to fix.</span>
        <span class="grow"></span>
        <button v-if="waiting" class="act" title="Stop waiting for Copilot (Mark done)" @click="stop">Stop waiting</button>
        <button v-else-if="herd.prReady(task) === 'draft'" class="act" title="Turn the draft PR into one ready for review and add the ready label" @click="herd.markReady(task)"><Icon name="check" :size="13" />Mark ready</button>
        <span v-else-if="herd.prReady(task) === 'ready'" class="muted small">ready for review</span>
      </div>
    </section>

    <section>
      <h3>CI checks</h3>
      <p v-if="!workflows.length" class="muted small">No checks reported yet (refreshed every minute).</p>
      <div v-for="w in workflows" :key="w.name" class="wf">
        <button class="line row" :title="w.cs[0].link ? 'Open on GitHub' : ''" @click="open(w.cs[0].link)">
          <span :class="['sdot', cls(w.bucket)]"></span>
          <span class="name">{{ w.name }}</span>
          <span class="muted small">{{ w.cs.length > 1 ? `${w.passed}/${w.cs.length} passed` : word(w.bucket) }}</span>
        </button>
        <button v-for="c in w.open" :key="c.name" class="sub" @click="open(c.link)">{{ c.name }} · {{ word(c.bucket) }}</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
  .tracker {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 18px 22px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    max-width: 760px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .title {
    font-size: 15px;
    font-weight: 600;
  }
  .small {
    font-size: 12px;
  }
  .grow {
    flex: 1;
  }
  h3 {
    margin: 0 0 8px;
    font-size: 12px;
    font-weight: 500;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 9px;
    font-size: 13px;
  }
  .row {
    width: 100%;
    padding: 6px 8px;
    margin-left: -8px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text);
    text-align: left;
  }
  .row:hover {
    background: var(--hover);
  }
  .name {
    flex: 1;
  }
  .sub {
    display: block;
    margin: 0 0 4px 18px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
    text-align: left;
  }
  .sub:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .sdot {
    flex: none;
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }
  .d-done { background: var(--done-dot); }
  .d-running { background: var(--running-dot); }
  .d-failed { background: var(--approval-dot); }
  .d-off { background: var(--faint); }
  .act {
    flex: none;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    padding: 0 9px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    font-size: 12px;
  }
  .act:hover {
    background: var(--hover);
  }
</style>
