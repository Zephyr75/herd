<script setup lang="ts">
// The task's PR tab: where its PR is in CI, refreshed every minute, and
// whether it is ready for review. Copilot's review and the failing checks'
// logs are the Copilot agent's artifacts (COPILOT-n.md, ERRORS-n.md).
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import { type CICheck, type Task } from '../lib/types'
import Icon from './ui/Icon.vue'

const props = defineProps<{ task: Task }>()

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

const open = (link: string) => link && herd.attempt('Could not open the link', () => api.OpenURL(link))
const refreshing = ref(false)
async function refresh() {
  refreshing.value = true
  await herd.attempt('Could not refresh PR #' + props.task.pr, () => api.RefreshPR(props.task.id))
  refreshing.value = false
}
</script>

<template>
  <div class="tracker">
    <div class="head">
      <span class="title">PR #{{ task.pr }}</span>
      <span class="muted small">{{ task.branch }} → {{ task.baseBranch }}</span>
      <span class="grow"></span>
      <button v-if="task.prState === 'open' && !task.archived" class="act" :disabled="refreshing" title="Check the PR and its CI checks now instead of waiting for the next minute" @click="refresh"><Icon name="rerun" :size="13" />{{ refreshing ? 'Refreshing…' : 'Refresh' }}</button>
      <button v-if="task.prUrl" class="act" :title="`Open PR #${task.pr} on GitHub`" @click="open(task.prUrl)"><Icon name="external" :size="13" />Open PR</button>
    </div>

    <section>
      <h3>Review</h3>
      <div class="line">
        <span :class="['sdot', herd.prReady(task) === 'ready' ? 'd-done' : 'd-off']"></span>
        <span v-if="herd.prReady(task) === 'ready'">Ready for review.</span>
        <span v-else-if="task.next.finished">A draft: the pipeline is finished, so it can go to review.</span>
        <span v-else>A draft, until the pipeline is finished.</span>
        <span class="grow"></span>
        <button v-if="herd.prReady(task) === 'draft'" class="act" title="Turn the draft PR into one ready for review and add the ready label" @click="herd.markReady(task)"><Icon name="check" :size="13" />Mark ready</button>
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
