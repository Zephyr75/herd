<script setup lang="ts">
// The PR at a glance, in the task's PR tab and as the Copilot step's view:
// Copilot's latest review and the CI checks, refreshed every minute, with
// buttons to ask Copilot again and to rerun checks. The review itself is the
// Copilot step's artifact (COPILOT-n.md).
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import { unsuccessful, type CICheck, type Option, type Step, type Task } from '../lib/types'
import Icon from './ui/Icon.vue'

const props = defineProps<{ task: Task; step?: Step }>()
// The Copilot step shown: the one opened, else the task's latest.
const cop = computed(() => props.step ?? [...props.task.steps].reverse().find((s) => s.external))
const live = computed(() => props.task.prState === 'open' && !props.task.archived)

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

const waiting = computed(() => cop.value?.state === 'running')
const open = (link: string) => link && herd.attempt('Could not open the link', () => api.OpenURL(link))
const stop = () => cop.value && herd.attempt('Could not stop waiting', () => api.MarkDone(cop.value!.id))
const askCopilot = () => herd.attempt('Could not ask Copilot for a review', () => api.RequestCopilotReview(props.task.id))

// Only GitHub Actions jobs can be rerun from herd; a workflow row reruns its whole run.
const actions = (link: string) => /\/actions\/runs\/\d+/.test(link)
const runOf = (link: string) => link.replace(/\/job\/.*$/, '')
const rerunning = ref('')
async function rerun(link: string, what: string) {
  rerunning.value = link || 'all'
  await herd.attempt('Could not rerun ' + what, () => api.RerunChecks(props.task.id, link))
  rerunning.value = ''
}
const anyActions = computed(() => props.task.checks.some((c) => actions(c.link)))

// Fixes go to the agent Copilot offers first when there is something to fix
// (Implement by default), as a new step with guidance.
const fixer = computed(() => {
  const ags = herd.settings?.agents ?? []
  const id = ags.find((a) => a.builtin)?.onFail[0] ?? 'impl'
  return ags.find((a) => a.id === id)
})
const busy = computed(() => props.task.steps.some((s) => !s.external && (s.state === 'running' || s.state === 'idle' || s.state === 'approval')))
const failing = computed(() => props.task.checks.filter((c) => c.bucket === 'fail' || c.bucket === 'cancel'))
const canFix = computed(() => live.value && !!fixer.value && !busy.value)
function fix(guidance: string) {
  const ag = fixer.value
  if (ag) herd.run({ agent: ag.id, label: ag.name, n: 0 } as Option, props.task.id, guidance)
}
const rebase = () =>
  fix(
    `Rebase this branch on origin/${props.task.baseBranch}: PR #${props.task.pr} conflicts with it, so GitHub runs no CI checks. ` +
      `git fetch origin ${props.task.baseBranch}, then git rebase origin/${props.task.baseBranch}; resolve each conflict keeping what both sides meant, ` +
      `and continue with git rebase --continue (the only commits allowed in this step). Run the tests of the files you resolved. ` +
      `Do not push: Commit and push does it. List the conflicts and how you resolved them.`,
  )
const fixReview = () =>
  fix(
    `Address Copilot's review of PR #${props.task.pr}, in ${cop.value?.artifact} in the run directory: for each comment, fix the code when it is right ` +
      `(apply its suggested change when there is one), and say in your artifact which comments you left and why. Fix the failing CI checks it lists too.`,
  )
const fixChecks = () =>
  fix(
    `Fix the failing CI checks of PR #${props.task.pr}: ${failing.value.map((c) => c.workflow ? `${c.workflow} / ${c.name}` : c.name).join(', ')}. ` +
      `Read their logs (gh run view <run id> --log-failed; the run id is in each check's link: ${failing.value.map((c) => c.link).filter(Boolean).join(' ')}), ` +
      `fix the cause in the code, or the test when the test is what is wrong, and run those tests locally when you can.`,
  )
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
      <button v-if="task.prState === 'open' && !task.archived" class="act" :disabled="refreshing" title="Check Copilot's review and the CI checks now instead of waiting for the next minute" @click="refresh"><Icon name="rerun" :size="13" />{{ refreshing ? 'Refreshing…' : 'Refresh' }}</button>
      <button v-if="task.prUrl" class="act" :title="`Open PR #${task.pr} on GitHub`" @click="open(task.prUrl)"><Icon name="external" :size="13" />Open PR</button>
    </div>

    <section>
      <h3>Copilot review</h3>
      <div class="line">
        <template v-if="cop">
          <span :class="['sdot', waiting ? 'd-running' : unsuccessful(cop) ? 'd-failed' : cop.verdict === 'skipped' ? 'd-off' : 'd-done']"></span>
          <span v-if="waiting">Waiting for Copilot's review and the CI checks (checked every minute); {{ cop.artifact }} gets both.</span>
          <span v-else-if="unsuccessful(cop)">Copilot comments or failing CI checks: see {{ cop.artifact }}.</span>
          <span v-else-if="cop.verdict === 'skipped'">herd stopped waiting; no review arrived.</span>
          <span v-else>Copilot found nothing and CI passed.</span>
        </template>
        <template v-else>
          <span class="sdot d-off"></span>
          <span>No Copilot review yet.</span>
        </template>
        <span class="grow"></span>
        <button v-if="waiting" class="act" title="Stop waiting for Copilot (Mark done)" @click="stop">Stop waiting</button>
        <button v-else-if="cop && unsuccessful(cop) && canFix" class="act" :title="`Start a ${fixer!.name} step that fixes what Copilot's review asks for`" @click="fixReview"><Icon name="pencil" :size="13" />Fix with {{ fixer!.name }}</button>
        <button v-if="live" class="act" title="Ask Copilot to review the PR as it is now; a Copilot step waits for the review" @click="askCopilot"><Icon name="rerun" :size="13" />{{ waiting ? 'Ask again' : 'Request review' }}</button>
      </div>
      <div v-if="herd.prReady(task)" class="line ready">
        <span :class="['sdot', herd.prReady(task) === 'ready' ? 'd-done' : 'd-off']"></span>
        <span>{{ herd.prReady(task) === 'ready' ? 'Ready for review.' : 'A draft.' }}</span>
        <span class="grow"></span>
        <button v-if="herd.prReady(task) === 'draft'" class="act" title="Turn the draft PR into one ready for review and add the ready label" @click="herd.markReady(task)"><Icon name="check" :size="13" />Mark ready</button>
      </div>
    </section>

    <section>
      <div class="h">
        <h3>CI checks</h3>
        <span class="grow"></span>
        <button v-if="canFix && failing.length && !task.conflicting" class="act" :title="`Start a ${fixer!.name} step that reads the failing checks' logs and fixes them`" @click="fixChecks"><Icon name="pencil" :size="13" />Fix with {{ fixer!.name }}</button>
        <button v-if="live && anyActions" class="act" :disabled="!!rerunning" title="Run every check again (each workflow run once)" @click="rerun('', 'the checks')"><Icon name="rerun" :size="13" />{{ rerunning === 'all' ? 'Rerunning…' : 'Rerun all' }}</button>
      </div>
      <div v-if="live && task.conflicting" class="line conflict">
        <Icon name="warn" :size="14" />
        <span>Merge conflicts with {{ task.baseBranch }}: GitHub runs no checks until they are resolved.</span>
        <span class="grow"></span>
        <button v-if="canFix" class="act" :title="`Start a ${fixer!.name} step that rebases the branch on origin/${task.baseBranch} and resolves the conflicts`" @click="rebase"><Icon name="branch" :size="13" />Rebase with {{ fixer!.name }}</button>
      </div>
      <p v-if="!workflows.length && !task.conflicting" class="muted small">No checks reported yet (refreshed every minute).</p>
      <div v-for="w in workflows" :key="w.name" class="wf">
        <div class="line rowwrap">
          <button class="line row" :title="w.cs[0].link ? 'Open on GitHub' : ''" @click="open(w.cs[0].link)">
            <span :class="['sdot', cls(w.bucket)]"></span>
            <span class="name">{{ w.name }}</span>
            <span class="muted small">{{ w.cs.length > 1 ? `${w.passed}/${w.cs.length} passed` : word(w.bucket) }}</span>
          </button>
          <button v-if="live && actions(w.cs[0].link)" class="icon" :disabled="!!rerunning" :title="`Rerun ${w.name}`" :aria-label="`Rerun ${w.name}`" @click="rerun(runOf(w.cs[0].link), w.name)"><Icon name="rerun" :size="13" /></button>
        </div>
        <div v-for="c in w.open" :key="c.name" class="subrow">
          <button class="sub" @click="open(c.link)">{{ c.name }} · {{ word(c.bucket) }}</button>
          <button v-if="live && actions(c.link) && w.cs.length > 1" class="icon" :disabled="!!rerunning" :title="`Rerun ${c.name} only`" :aria-label="`Rerun ${c.name}`" @click="rerun(c.link, c.name)"><Icon name="rerun" :size="12" /></button>
        </div>
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
  .act:disabled,
  .icon:disabled {
    opacity: 0.5;
  }
  .h {
    display: flex;
    align-items: center;
    margin-bottom: 8px;
  }
  .h h3 {
    margin: 0;
  }
  .ready {
    margin-top: 8px;
  }
  .conflict {
    margin-bottom: 8px;
    color: var(--waiting);
  }
  .h .act + .act {
    margin-left: 6px;
  }
  .rowwrap {
    gap: 4px;
  }
  .subrow {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 0 0 4px 18px;
  }
  .subrow .sub {
    margin: 0;
  }
  .icon {
    flex: none;
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--muted);
  }
  .icon:hover {
    background: var(--hover);
    color: var(--text);
  }
</style>
