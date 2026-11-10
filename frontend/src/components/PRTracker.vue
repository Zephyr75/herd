<script setup lang="ts">
// The PR at a glance, in the task's PR tab and as the Copilot step's view:
// Copilot's latest review and the CI checks, refreshed every minute, with
// buttons to ask Copilot again and to rerun checks. The review itself is the
// Copilot step's artifact (COPILOT-n.md).
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { herd } from '../lib/state'
import { unsuccessful, type CICheck, type Feedback, type Step, type Task } from '../lib/types'
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
const fixer = computed(() => herd.fixers()[0])
const failing = computed(() => props.task.checks.filter((c) => c.bucket === 'fail' || c.bucket === 'cancel'))
const canFix = computed(() => live.value && herd.canFix(props.task))
const fix = (guidance: string) => herd.fixWith(props.task, guidance)

// Review comments: every one is sent unless unticked.
// Ticked by default unless outdated (the code it points at changed since);
// flipped holds the ones the user ticked or unticked.
const flipped = ref(new Set<string>())
const ticked = (f: Feedback) => f.outdated === flipped.value.has(f.id)
const toggle = (id: string) => {
  const s = new Set(flipped.value)
  if (!s.delete(id)) s.add(id)
  flipped.value = s
}
const openFb = computed(() => props.task.feedback.filter((f) => !f.resolvedBy))
const resolvedFb = computed(() => props.task.feedback.filter((f) => f.resolvedBy))
const showResolved = ref(false)
const picked = computed(() => openFb.value.filter(ticked))
// Resolve marks a thread resolved on GitHub; it then moves to the resolved ones.
const resolving = ref('')
async function resolve(f: Feedback) {
  resolving.value = f.id
  await herd.attempt('Could not resolve the comment', () => api.ResolveThread(props.task.id, f.id))
  resolving.value = ''
}
const where = (f: Feedback) => (f.path ? `${f.path}:${f.line}` : 'on the PR')
function fixFeedback() {
  const comments = picked.value
    .map((f) => {
      const replies = f.replies.map((r) => `> ${r.author}: ${r.body.replace(/\n/g, '\n> ')}`).join('\n')
      return `### ${where(f)}${f.outdated ? ' (outdated: the code changed since)' : ''}, by ${f.author}\n${f.body}${replies ? '\n\n' + replies : ''}`
    })
    .join('\n\n')
  fix(herd.prompt('fixFeedback', { pr: String(props.task.pr), comments }))
}
const rebase = () => fix(herd.prompt('rebase', { pr: String(props.task.pr), base: props.task.baseBranch, behind: String(props.task.behind || 'some') }))
const fixChecks = () =>
  fix(
    herd.prompt('fixChecks', {
      pr: String(props.task.pr),
      checks: failing.value.map((c) => (c.workflow ? `${c.workflow} / ${c.name}` : c.name)).join(', '),
      links: failing.value.map((c) => c.link).filter(Boolean).join(' '),
    }),
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
      <span v-if="herd.prReady(task)" :class="['state', herd.prReady(task)]" :title="herd.prReady(task) === 'ready' ? 'Ready for review' : 'A draft: reviewers and repo bots leave it alone until it is marked ready'">{{ herd.prReady(task) === 'ready' ? 'Ready' : 'Draft' }}</span>
      <span class="muted small">{{ task.branch }} → {{ task.baseBranch }}</span>
      <span class="grow"></span>
      <button v-if="herd.prReady(task) === 'draft'" class="act" title="Turn the draft PR into one ready for review and add the ready label" @click="herd.markReady(task)"><Icon name="check" :size="13" />Mark ready</button>
      <button v-if="task.prState === 'open' && !task.archived" class="act" :disabled="refreshing" title="Check Copilot's review and the CI checks now instead of waiting for the next minute" @click="refresh"><Icon name="rerun" :size="13" />{{ refreshing ? 'Refreshing…' : 'Refresh' }}</button>
      <button v-if="task.prUrl" class="act" :title="`Open PR #${task.pr} on GitHub`" @click="open(task.prUrl)"><Icon name="external" :size="13" />Open PR</button>
    </div>

    <section>
      <h3>Copilot review</h3>
      <div class="line">
        <template v-if="cop">
          <span :class="['sdot', waiting ? 'd-running' : unsuccessful(cop) ? 'd-failed' : cop.verdict === 'skipped' ? 'd-off' : 'd-done']"></span>
          <span v-if="waiting">Waiting for Copilot's review and the CI checks (checked every minute); {{ cop.artifact }} gets both.</span>
          <span v-else-if="unsuccessful(cop)">Copilot comments or failing CI checks ({{ cop.artifact }}): fix them from Review comments and CI checks below.</span>
          <span v-else-if="cop.verdict === 'skipped'">herd stopped waiting; no review arrived.</span>
          <span v-else>Copilot found nothing and CI passed.</span>
        </template>
        <template v-else>
          <span class="sdot d-off"></span>
          <span>No Copilot review yet.</span>
        </template>
        <span class="grow"></span>
        <button v-if="waiting" class="act" title="Stop waiting for Copilot (Mark done)" @click="stop">Stop waiting</button>
        <button v-if="live" class="act" title="Ask Copilot to review the PR as it is now; a Copilot step waits for the review" @click="askCopilot"><Icon name="rerun" :size="13" />{{ waiting ? 'Ask again' : 'Request review' }}</button>
      </div>
    </section>

    <section v-if="task.feedback.length">
      <div class="h">
        <h3>Review comments</h3>
        <span class="grow"></span>
        <button v-if="canFix && openFb.length" class="act" :disabled="!picked.length" :title="`Start a ${fixer!.name} step that addresses the ticked comments`" @click="fixFeedback"><Icon name="pencil" :size="13" />Fix {{ picked.length }} with {{ fixer!.name }}</button>
      </div>
      <p v-if="!openFb.length" class="muted small">None open.</p>
      <div v-for="f in [...openFb, ...(showResolved ? resolvedFb : [])]" :key="f.id" :class="['fb', { resolved: f.resolvedBy }]">
        <input v-if="!f.resolvedBy" type="checkbox" :checked="ticked(f)" :aria-label="'Send the comment of ' + f.author" @change="toggle(f.id)" />
        <Icon v-else name="check" :size="13" class="done" />
        <div class="fbody">
          <div class="fmeta">
            <span class="who">{{ f.author }}</span>
            <span class="mono muted">{{ where(f) }}</span>
            <span v-if="f.resolvedBy" class="muted">resolved by {{ f.resolvedBy }}</span>
            <span v-else-if="f.outdated" class="muted">outdated</span>
            <span v-if="f.replies.length" class="muted">· {{ f.replies.length }} {{ f.replies.length > 1 ? 'replies' : 'reply' }}</span>
            <span class="grow"></span>
            <button v-if="live && f.path && !f.resolvedBy" class="icon" :disabled="resolving === f.id" title="Mark resolved on GitHub" aria-label="Mark resolved on GitHub" @click="resolve(f)"><Icon name="check" :size="15" /></button>
            <button class="icon" title="Open on GitHub" aria-label="Open on GitHub" @click="open(f.url)"><Icon name="external" :size="12" /></button>
          </div>
          <div class="ftext" :title="f.body">{{ f.body }}</div>
        </div>
      </div>
      <button v-if="resolvedFb.length" class="more muted small" :aria-expanded="showResolved" @click="showResolved = !showResolved">
        <Icon :name="showResolved ? 'chevron' : 'chevron-right'" :size="12" />{{ resolvedFb.length }} resolved
      </button>
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
        <button v-if="canFix" class="act" :title="`Start a ${fixer!.name} step that rebases the branch on origin/${task.baseBranch} and resolves the conflicts`" @click="rebase"><Icon name="rebase" :size="13" />Rebase with {{ fixer!.name }}</button>
      </div>
      <p v-if="!workflows.length && !task.conflicting" class="muted small">No checks reported yet (refreshed every minute).</p>
      <div v-for="w in workflows" :key="w.name" class="wf">
        <div class="line rowwrap">
          <button class="line row" :title="w.cs[0].link ? 'Open on GitHub' : ''" @click="open(w.cs[0].link)">
            <span :class="['sdot', cls(w.bucket)]"></span>
            <span class="name">{{ w.name }}</span>
            <span class="muted small">{{ w.cs.length > 1 ? `${w.passed}/${w.cs.length} passed` : word(w.bucket) }}</span>
          </button>
          <button v-if="live && actions(w.cs[0].link)" class="icon" :disabled="!!rerunning" :title="`Rerun ${w.name}`" :aria-label="`Rerun ${w.name}`" @click="rerun(runOf(w.cs[0].link), w.name)"><Icon name="rerun" :size="15" /></button>
        </div>
        <div v-for="c in w.open" :key="c.name" class="subrow">
          <button class="sub" @click="open(c.link)">{{ c.name }} · {{ word(c.bucket) }}</button>
          <button v-if="live && actions(c.link) && w.cs.length > 1" class="icon" :disabled="!!rerunning" :title="`Rerun ${c.name} only`" :aria-label="`Rerun ${c.name}`" @click="rerun(c.link, c.name)"><Icon name="rerun" :size="14" /></button>
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
  .state {
    padding: 1px 7px;
    border: 1px solid var(--border);
    border-radius: 10px;
    font-size: 11.5px;
    color: var(--muted);
  }
  .state.ready {
    border-color: var(--done);
    color: var(--done);
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
  .fb {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 6px 0;
    border-top: 1px solid var(--border);
  }
  .fb:first-of-type {
    border-top: 0;
  }
  .fb input {
    margin-top: 3px;
  }
  .fb .done {
    margin-top: 2px;
    flex: none;
    color: var(--muted);
  }
  .fb.resolved .ftext {
    color: var(--muted);
  }
  .more {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 4px;
    padding: 2px 0;
  }
  .more:hover {
    color: var(--text);
  }
  .fbody {
    flex: 1;
    min-width: 0;
  }
  .fmeta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }
  .who {
    font-weight: 500;
  }
  .fmeta .mono {
    font-family: var(--mono);
    font-size: 11.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ftext {
    margin-top: 2px;
    font-size: 12.5px;
    line-height: 1.45;
    white-space: pre-wrap;
    display: -webkit-box;
    -webkit-line-clamp: 4;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .icon:hover {
    background: var(--hover);
    color: var(--text);
  }
</style>
