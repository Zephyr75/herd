// Browser-only stand-in for the Go service, using the UI preview's data as
// fixtures. It mirrors internal/app/views.go closely enough to click through
// every screen; it never touches git, gh or Claude Code.

import type { Api } from './api'
import type { Agent, Artifact, Context, Next, Project, Settings, Step, Task, StatusKind, Usage } from './types'

type Handler = (data: any) => void
const listeners = new Map<string, Set<Handler>>()
export function on(event: string, fn: Handler) {
  if (!listeners.has(event)) listeners.set(event, new Set())
  listeners.get(event)!.add(fn)
  return () => listeners.get(event)!.delete(fn)
}
function emit(event: string, data: unknown) {
  listeners.get(event)?.forEach((fn) => fn(data))
}

const projects: Project[] = [
  { id: 'core', name: 'pony-core', path: '~/code/pony/pony-core', repo: 'pony-org/pony-core', baseBranch: 'main', worktreesDir: '.worktrees' },
  { id: 'rider', name: 'pony-rider-app', path: '~/code/pony/rider-app', repo: 'pony-org/rider-app', baseBranch: 'develop', worktreesDir: '.worktrees' },
  { id: 'infra', name: 'pony-infra', path: '~/code/pony/infra', repo: 'pony-org/infra', baseBranch: 'main', worktreesDir: '.worktrees' },
]
const ISSUE_TITLES: Record<string, string> = { '1190': 'Pause rides when the battery drops under 5%', '4356': 'Retry BLE unlock on flaky connections' }

const agents: Agent[] = [
  { id: 'plan', name: 'Plan', command: '/herd:plan {task} -- {guidance}', model: 'sonnet', trigger: 'manual', writes: 'PLAN.md', preview: '', done: 'status: done', commit: false, push: false,
    instructions: 'Read the task source: the GitHub issue, the description, or both.\nExplore the repo and decide an approach.\nWrite PLAN.md with Goal, Approach (a mermaid flowchart), Files and Out of scope.\nDo not edit code. End with front matter status: done.' },
  { id: 'impl', name: 'Implement', command: '/herd:implement {task} -- {guidance}', model: 'opus', trigger: 'manual', writes: 'IMPLEMENT-{n}.md', preview: '', done: 'status: done', commit: false, push: false,
    instructions: 'Read PLAN.md, and FEEDBACK.md when it exists.\nImplement the plan in this worktree and run the relevant tests.\nDo not commit: herd commits your changes when the Review step ends.\nWrite IMPLEMENT-{n}.md with Changes, Tests and Notes for the reviewer. End with status: done.' },
  { id: 'review', name: 'Review', command: '/herd:review {task}', model: 'opus', trigger: 'after:impl', writes: 'REVIEW-{n}.md', preview: '', done: 'verdict: approve | changes', commit: true, push: false,
    instructions: 'Review the uncommitted changes in this worktree (git status, git diff HEAD) against PLAN.md.\nList findings, most important first, and append them to FEEDBACK.md.\nWrite REVIEW-{n}.md with front matter verdict: approve or changes.\nNever edit code: herd commits the reviewed changes when you finish.' },
  { id: 'push', name: 'Push', command: '/herd:push {task}', model: 'haiku', trigger: 'manual', writes: 'PR.md', preview: 'FEEDBACK.md', done: 'status: done', commit: false, push: true,
    instructions: 'Write PR.md: a title line, then a body summarising PLAN.md and the IMPLEMENT files.\nAdd Closes #{issue} when the task has an issue.\nDo not push: herd pushes, opens the PR and collects the Copilot review.' },
]

const context: Record<string, Context> = {
  general: { id: 'general', text: '# General context\n- Match the style already in the repo; no new dependencies without asking.\n- Never commit or push: herd does it.\n- Artifacts go to $HERD_RUN_DIR and start with YAML front matter.\n- Prefer small diffs; explain trade-offs in the artifact, not in code comments.\n', claudeMd: false, knowledge: '' },
  core: { id: 'core', text: '# pony-core\n- Go services live in internal/<domain>.\n- Run go test ./internal/<domain>/... before writing IMPLEMENT.\n- Rider-facing errors go through internal/errs.\n', claudeMd: true, knowledge: '' },
  rider: { id: 'rider', text: '# pony-rider-app\n- React Native, TypeScript strict.\n- UI copy lives in src/i18n; never hardcode strings.\n- Run pnpm test <area> before writing IMPLEMENT.\n', claudeMd: true, knowledge: '' },
  infra: { id: 'infra', text: '# pony-infra\n- Terraform 1.9, azurerm provider.\n- Never run terraform apply; validate and plan only.\n- Tag every resource with team = mobility.\n', claudeMd: false, knowledge: '' },
}

const settings: Settings = {
  config: {
    appearance: { theme: 'dark', uiFont: 'IBM Plex Sans', uiFontSize: 13, termFont: 'IBM Plex Mono', termFontSize: 12.5 },
    zedCommand: 'zed', claudeCommand: 'claude', maxLiveSessions: 6, copilotTimeoutMinutes: 30, notifications: true, maxTabs: 5,
    rules: 'Never edit generated files under gen/.\nRun the tests you touched before writing the artifact.', projects,
  },
  agents, context,
  keymap: {
    palette: ['Mod+P'], scope: ['Mod+Alt+O'], newTask: ['Mod+N'], runPrimary: ['Mod+Enter'], toggleArchive: ['Mod+Shift+A'],
    nextTask: ['Alt+ArrowDown'], prevTask: ['Alt+ArrowUp'], tab1: ['Alt+1'], tab2: ['Alt+2'], tab3: ['Alt+3'], tab4: ['Alt+4'],
    tab5: ['Alt+5'], tab6: ['Alt+6'], tab7: ['Alt+7'], tab8: ['Alt+8'], tab9: ['Alt+9'], closeTab: ['Mod+W'],
    nextTab: ['Ctrl+PageDown', 'Ctrl+Tab'], prevTab: ['Ctrl+PageUp', 'Ctrl+Shift+Tab'],
    openWorktree: ['Mod+Shift+E'], openDiff: ['Mod+Shift+D'], settings: ['Mod+,'], close: ['Escape'],
  },
  dir: '~/.herd',
}

interface Fx {
  goal?: string; files?: string[]; grep?: string; test?: string; testOut?: string; flow?: string[]
  diff?: [string, string][]; changes?: string[]; findings?: string[]; copilot?: string[]
}
interface MockStep { key: string; agent: string; n: number; state: Step['state']; guide?: string; session: string; sha?: string; verdict?: string; manual?: boolean }
interface MockTask { id: string; project: string; issue: string; slug: string; title: string; description: string; archived?: boolean; pr?: number; prState?: string; archivedOn?: string; steps: MockStep[]; fx: Fx; removed?: boolean }

const T: MockTask[] = [
  { id: 'core:4356', project: 'core', issue: '4356', slug: '4356', title: 'Retry BLE unlock on flaky connections', description: '',
    fx: { goal: 'Unlock requests fail on scooters with older BLE firmware when the first connection attempt drops. Add bounded retries with backoff before surfacing an error to the rider.',
      files: ['internal/unlock/ble.go', 'internal/unlock/retry.go', 'internal/unlock/ble_test.go'], grep: 'UnlockVehicle', test: 'go test ./internal/unlock/...', testOut: 'ok   pony-core/internal/unlock   1.84s',
      flow: ['Unlock request', 'Connect over BLE', 'Connected?', 'Retry with backoff, max N', 'Return unlock result'],
      diff: [['+', 'func withRetry(ctx context.Context, budget int, fn func() error) error {'], ['+', '\tbackoff := 200 * time.Millisecond'], ['-', '\treturn conn.Unlock(ctx)'], ['+', '\treturn withRetry(ctx, cfg.RetryBudget, func() error { return conn.Unlock(ctx) })']],
      changes: ['Added withRetry helper with exponential backoff', 'Unlock path wraps the BLE call in withRetry', 'Tests for success after one drop'],
      findings: ['Retry budget is hardcoded to 3, should come from config', 'Backoff ignores context cancellation', 'No test for the exhausted-budget path'] },
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', guide: 'focus on scooters with old firmware', session: '1a9e42c0' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: '5d02ab17' },
      { key: 'review-1', agent: 'review', n: 1, state: 'done', sha: 'b7c41d9', verdict: 'changes', session: 'c83f0e55' },
      { key: 'impl-2', agent: 'impl', n: 2, state: 'running', guide: 'keep the retry budget configurable, default 3', session: '7f3c91d4' },
    ] },
  { id: 'core:4361', project: 'core', issue: '4361', slug: '4361', title: 'Per-minute pricing rounding', description: 'Keep the per-minute rule identical to the B2B tariff.',
    fx: { goal: 'Ride prices are rounded per second in one code path and per minute in another. Make per-minute ceiling rounding the single rule.',
      files: ['internal/pricing/minute.go', 'internal/pricing/minute_test.go'], grep: 'RoundDuration', test: 'go test ./internal/pricing/...', testOut: 'ok   pony-core/internal/pricing   0.62s',
      flow: ['Ride ends', 'Compute duration', 'Ceil to minute', 'Apply tariff'], diff: [['-', '\tsecs := d.Seconds()'], ['+', '\tmins := decimal.NewFromFloat(d.Minutes()).Ceil()']],
      changes: ['Single RoundDuration helper', 'Removed per-second path', 'Table tests for 0s, 59s, 60s, 61s'], findings: ['Rounding uses decimal.Ceil consistently', 'Zero-length ride is covered', 'Nothing blocking'] },
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '2b77e010' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: '44e1c9b2' },
      { key: 'review-1', agent: 'review', n: 1, state: 'done', sha: '91aa0f3', verdict: 'approve', session: 'e0c5d7a8' },
    ] },
  { id: 'core:4370', project: 'core', issue: '4370', slug: '4370', title: 'Batch battery telemetry ingestion', description: '',
    fx: { goal: 'Telemetry rows are inserted one by one. Batch them per vehicle every 5 seconds to cut database writes.', files: ['internal/telemetry/ingest.go', 'internal/telemetry/batch.go'],
      grep: 'InsertTelemetry', test: 'go test -race ./internal/telemetry/...', flow: ['Telemetry message', 'Buffer per vehicle', 'Flush on 5s or 500 rows', 'Bulk insert'],
      diff: [['+', 'type batcher struct {'], ['+', '\tbuf map[string][]Row'], ['-', '\tdb.InsertTelemetry(ctx, row)']], changes: ['New batcher with time and size flush'] },
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '9c1d3e66' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'approval', session: 'aa20f4b1' },
    ] },
  { id: 'core:local-heatmap', project: 'core', issue: '', slug: 'local-heatmap', title: 'Fleet rebalancing heatmap endpoint',
    description: 'Add GET /fleet/heatmap returning vehicle density per H3 cell for the ops dashboard.', fx: {}, steps: [] },
  { id: 'rider:1188', project: 'rider', issue: '1188', slug: '1188', title: 'Show unlock retry progress in the app', description: '',
    fx: { goal: 'When the backend retries a BLE unlock, the rider sees nothing for up to 6 seconds. Show the attempt count in the unlock sheet.', files: ['src/unlock/UnlockSheet.tsx', 'src/unlock/useUnlock.ts'],
      grep: 'useUnlock', test: 'pnpm test unlock', testOut: 'Tests  12 passed', flow: ['Tap unlock', 'Show progress sheet', 'Retry event from API', 'Update attempt count', 'Success or error state'] },
    steps: [{ key: 'plan-1', agent: 'plan', n: 1, state: 'running', session: '6e0d1f93' }] },
  { id: 'rider:local-map-sdk', project: 'rider', issue: '', slug: 'local-map-sdk', title: 'Remove deprecated map SDK calls',
    description: 'Replace MapView.setCamera calls deprecated in map SDK 11 with the new camera API. No visual change.',
    fx: { goal: 'Map SDK 11 deprecates MapView.setCamera. Move every call to the camera API without changing what riders see.', files: ['src/map/RideMap.tsx', 'src/map/camera.ts'], grep: 'setCamera',
      test: 'pnpm test map', testOut: 'Tests  8 passed', flow: ['Find setCamera calls', 'Swap to camera API', 'Snapshot tests'] },
    steps: [{ key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '3c72aa05' }] },
  { id: 'infra:214', project: 'infra', issue: '214', slug: '214', title: 'AKS node pool for telemetry ingestion', description: 'Use spot instances where possible.',
    fx: { goal: 'Telemetry ingestion shares the default node pool. Give it a dedicated autoscaling pool, on spot instances where possible.', files: ['modules/aks/nodepool.tf', 'envs/prod/aks.tf'],
      grep: 'azurerm_kubernetes_cluster_node_pool', test: 'terraform validate', testOut: 'Success! The configuration is valid.', flow: ['Ingestion pods', 'Taint and toleration', 'Spot pool, 1 to 6 nodes', 'Fallback to regular pool?'],
      diff: [['+', 'resource "azurerm_kubernetes_cluster_node_pool" "ingest" {'], ['+', '  priority        = "Spot"'], ['+', '  eviction_policy = "Delete"']],
      changes: ['New ingest node pool module call', 'Taint ingest=true:NoSchedule'], findings: ['Spot eviction needs a PodDisruptionBudget'] },
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '81fe2a3c' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: 'b5c0d9e1' },
      { key: 'review-1', agent: 'review', n: 1, state: 'running', session: '0d9c3b74' },
    ] },
  { id: 'core:4349', project: 'core', issue: '4349', slug: '4349', title: 'Geofence import from city GeoJSON', description: '', archived: true, pr: 812, prState: 'merged', archivedOn: '2026-10-28',
    fx: { goal: 'Import parking and no-ride zones from the city open-data GeoJSON instead of the manual CSV.', files: ['internal/zones/geojson.go', 'internal/zones/import.go'], test: 'go test ./internal/zones/...',
      testOut: 'ok   pony-core/internal/zones   1.12s', flow: ['Fetch GeoJSON', 'Validate polygons', 'Upsert zones'], diff: [['+', 'func ParseGeoJSON(r io.Reader) ([]Zone, error) {']],
      changes: ['GeoJSON parser', 'Import command uses the parser'], findings: ['Polygon winding order validated'],
      copilot: ['Consider closing the response body with defer in fetch', 'ParseGeoJSON could return a typed error for invalid rings'] },
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '0f4b8a21' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: '3e9a77c0' },
      { key: 'review-1', agent: 'review', n: 1, state: 'done', sha: '6d3e20b', verdict: 'approve', session: '71bc04de' },
      { key: 'push-1', agent: 'push', n: 1, state: 'done', session: 'd2a6f5e9' },
    ] },
  { id: 'infra:207', project: 'infra', issue: '207', slug: '207', title: 'Rotate SP certificates through Horizon', description: '', archived: true, pr: 64, prState: 'merged', archivedOn: '2026-10-21', fx: {},
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '5a1b2c3d' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: '6b2c3d4e' },
      { key: 'review-1', agent: 'review', n: 1, state: 'done', sha: '2c8e7f1', verdict: 'approve', session: '7c3d4e5f' },
      { key: 'push-1', agent: 'push', n: 1, state: 'done', session: '8d4e5f60' },
    ] },
  { id: 'rider:1170', project: 'rider', issue: '1170', slug: '1170', title: 'Dark mode for ride summary', description: '', archived: true, pr: 301, prState: 'closed', archivedOn: '2026-10-17', fx: {},
    steps: [
      { key: 'plan-1', agent: 'plan', n: 1, state: 'done', session: '9e5f6071' },
      { key: 'impl-1', agent: 'impl', n: 1, state: 'done', session: 'af607182' },
      { key: 'review-1', agent: 'review', n: 1, state: 'done', sha: 'a4f0b12', verdict: 'approve', session: 'b0718293' },
      { key: 'push-1', agent: 'push', n: 1, state: 'done', session: 'c1829304' },
    ] },
]

const proj = (id: string) => projects.find((p) => p.id === id) ?? projects[0]
const agent = (id: string) => agents.find((a) => a.id === id) ?? agents[0]
const displayName = (a: Agent, n: number) => (a.writes.includes('{n}') ? `${a.name} ${n}` : a.name)
const fmtDay = (iso: string) => new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })

// Plausible token usage, stable per session id.
function mockUsage(session: string, running: boolean): Usage {
  const n = parseInt(session.slice(0, 4), 16) || 1
  const u = { input: 40 + (n % 900), output: 3000 + n * 2, cacheRead: 120000 + n * 60, cacheWrite: 30000 + n * 4 }
  if (running) u.output = Math.round(u.output / 3)
  return u
}
const addUsage = (a: Usage, b: Usage): Usage => ({ input: a.input + b.input, output: a.output + b.output, cacheRead: a.cacheRead + b.cacheRead, cacheWrite: a.cacheWrite + b.cacheWrite })
const noUsage: Usage = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }

function stepView(t: MockTask, s: MockStep, i: number): Step {
  const a = agent(s.agent)
  const open = s.state !== 'done' && s.state !== 'failed'
  const paused = new URLSearchParams(location.search).get('paused') === `${t.id}/${s.key}` || (!!s.manual && open) // ?paused=<step id>
  return {
    id: `${t.id}/${s.key}`, taskId: t.id, key: s.key, agent: s.agent, n: s.n, seq: i + 1, state: s.state, guidance: s.guide ?? '',
    session: s.session, sha: s.sha ?? '', verdict: s.verdict ?? '', model: a.model, completed: s.state === 'done', startedAt: '', endedAt: '',
    name: displayName(a, s.n), live: s.state !== 'done' && !paused, auto: !!a.trigger.startsWith('after:'),
    artifact: a.writes.replace('{n}', String(s.n)), preview: (a.preview || a.writes).replace('{n}', String(s.n)), commits: a.commit,
    paused, manual: !!s.manual, fresh: !!s.manual && open, usage: mockUsage(s.session, s.state !== 'done'),
  }
}

function derive(t: MockTask): Task {
  const p = proj(t.project)
  const steps = t.steps.map((s, i) => stepView(t, s, i))
  const last = steps[steps.length - 1]
  let status: Task['status']
  const day = t.archivedOn ? fmtDay(t.archivedOn) : ''
  const st = (kind: StatusKind, label: string, short: string) => ({ kind, label, short })
  if (t.archived) status = t.pr ? st(t.prState === 'merged' ? 'done' : 'idle', `PR #${t.pr} ${t.prState} · ${day}`, t.prState!) : st('idle', `Archived · ${day}`, 'archived')
  else if (!steps.length) status = st('idle', 'Not started', 'not started')
  else if (last.manual && last.paused) status = st('waiting', 'Manual · ' + last.name, 'manual')
  else if (steps.some((s) => s.state === 'approval')) status = st('approval', 'Needs approval · ' + steps.find((s) => s.state === 'approval')!.name, 'approval')
  else if (steps.some((s) => s.state === 'running')) status = st('running', 'Running · ' + steps.find((s) => s.state === 'running')!.name, 'running')
  else if (last.state === 'idle') status = st('waiting', 'Waiting for input · ' + last.name, 'waiting')
  else if (last.state === 'failed') status = st('waiting', `Waiting for input · ${last.name} failed`, 'failed')
  else if (t.pr && t.prState === 'open') status = st('done', `PR #${t.pr} open`, 'PR open')
  else status = st('waiting', 'Waiting for input · after ' + last.name, 'waiting')

  const next: Next = { options: [], text: '', approval: '', markDone: '', ghost: null as Next['ghost'] }
  const count = (id: string) => Math.max(0, ...steps.filter((s) => s.agent === id).map((s) => s.n))
  const opt = (a: Agent, n: number, prefix = '') => ({ agent: a.id, n, label: prefix + displayName(a, n) })
  if (t.archived) {
    next.text = (t.pr ? `Archived ${day} when PR #${t.pr} was ${t.prState}.` : `Archived ${day}.`) + (t.removed ? ' The worktree and branch were removed.' : ' The worktree is kept until you remove it.')
  } else if (!last) next.options.push(opt(agents[0], 1))
  else {
    const la = agent(last.agent)
    const autoNext = agents.find((a) => a.trigger === 'after:' + last.agent)
    if (autoNext && last.state !== 'done') next.ghost = { name: displayName(autoNext, count(autoNext.id) + 1), after: last.name, manual: false, start: null }
    const manualNext = autoNext && last.manual && last.state !== 'failed' ? opt(autoNext, count(autoNext.id) + 1) : undefined
    if (manualNext) next.ghost = { name: manualNext.label, after: last.name, manual: true, start: manualNext }
    const appr = steps.find((s) => s.state === 'approval')
    const run = steps.find((s) => s.state === 'running')
    if (appr) { next.approval = appr.id; next.text = 'Claude Code wants to run a command. Answer in the terminal, or allow it once here.' }
    else if (run) next.text = `${run.name} is running. ` + (autoNext && run.id === last.id ? `${displayName(autoNext, count(autoNext.id) + 1)} starts automatically when it finishes.` : 'You will be asked for the next step when it finishes.')
    else if (manualNext) { next.options.push(manualNext); next.text = `${last.name} is yours: use its session outside herd (copy the claude command in its toolbar) or Resume it here. Start ${manualNext.label} when you are done.` }
    else if (last.state === 'idle') { next.text = `Claude is waiting for you in the ${last.name} terminal. Mark it done if ${last.artifact} is complete.`; next.markDone = last.id }
    else if (last.state === 'failed') { next.text = `${last.name} ended before ${last.artifact} was done.`; next.options.push(opt(la, last.n, 'Retry ')); next.markDone = last.id }
    else if (la.push) next.text = `PR #${t.pr} is open. The task moves to Archive when the PR is merged or closed.`
    else {
      const idx = agents.indexOf(la)
      const nextA = agents.slice(idx + 1).find((a) => !a.trigger.startsWith('after:'))
      const loopId = la.trigger.startsWith('after:') ? la.trigger.slice(6) : ''
      const loopA = last.verdict ? agents.find((a) => a.id === loopId) : undefined
      const opts = nextA ? [opt(nextA, count(nextA.id) + 1)] : []
      if (loopA) { const lo = opt(loopA, count(loopA.id) + 1); if (last.verdict === 'approve') opts.push(lo); else opts.unshift(lo) }
      next.options = opts
    }
  }
  return {
    id: t.id, project: t.project, slug: t.slug, issue: t.issue, title: t.title, description: t.description, createdAt: '',
    archived: !!t.archived, archivedAt: t.archivedOn ?? '', pr: t.pr ?? 0, prUrl: '', prState: t.prState ?? '', worktreeRemoved: !!t.removed,
    projectName: p.name, repo: p.repo, baseBranch: p.baseBranch, worktree: `${p.path}/.worktrees/${t.slug}`, worktreeRel: `.worktrees/${t.slug}`,
    branch: 'agent/' + t.slug, runDir: `~/.herd/runs/${p.name}/${t.slug}`,
    source: t.issue && t.description ? `GitHub issue #${t.issue} + description · ${p.repo}` : t.issue ? `GitHub issue #${t.issue} · ${p.repo}` : 'LOCAL · description only, no GitHub issue',
    status, steps, next, usage: steps.reduce((u, s) => addUsage(u, s.usage), noUsage),
  }
}

const find = (id: string) => T.find((t) => t.id === id)!
const changed = (t: MockTask) => emit('task:changed', derive(t))
const later = (ms: number, fn: () => void) => setTimeout(fn, ms)

function prevSha(t: MockTask, key: string) {
  let sha = ''
  for (const s of t.steps) { if (s.key === key) break; if (s.sha) sha = s.sha }
  return sha
}

// ---- mock terminal and artifacts ----

const C = { reset: '\x1b[0m', bold: '\x1b[1m', dim: '\x1b[2m', red: '\x1b[31m', green: '\x1b[32m', yellow: '\x1b[33m', cyan: '\x1b[36m', grey: '\x1b[90m', addBg: '\x1b[42;30m', delBg: '\x1b[41;30m' }

export function mockTerminal(stepId: string): string {
  const [tid, key] = [stepId.slice(0, stepId.lastIndexOf('/')), stepId.slice(stepId.lastIndexOf('/') + 1)]
  const t = find(tid), s = t.steps.find((x) => x.key === key)!
  const fx = t.fx, p = proj(t.project), run = `~/.herd/runs/${p.name}/${t.slug}`
  const files = fx.files ?? ['README.md'], test = fx.test ?? 'make test'
  const out: string[] = []
  const L = (x: string, c = '') => out.push(c + x + C.reset)
  const cmd = s.agent === 'impl' ? 'implement' : s.agent
  L(`> /herd:${cmd} ${t.issue || '--local'}${s.guide ? ' -- ' + s.guide : ''}`, C.bold)
  L('')
  if (s.agent === 'plan') {
    if (t.issue) { L(`⏺ Bash(gh issue view ${t.issue} --repo ${p.repo})`); L(`  ⎿ "${t.title}" · 3 comments`, C.grey) }
    if (t.description) { L('⏺ Task description'); L('  ⎿ ' + t.description, C.grey) }
    L(`⏺ Search(pattern: "${fx.grep ?? t.title.split(' ')[0]}")`); L(`  ⎿ Found ${files.length + 4} files`, C.grey)
    files.slice(0, 2).forEach((f) => L(`⏺ Read(${f})`))
    if (s.state === 'running') { L(''); L('✻ Writing PLAN.md…  (esc to interrupt)', C.yellow) }
    else { L(`⏺ Write(${run}/PLAN.md)`); L('  ⎿ Wrote 58 lines · status: done', C.grey); L(''); L('● Step complete · waiting for you to start Implement 1', C.cyan) }
  } else if (s.agent === 'impl') {
    L(`⏺ Read(${run}/PLAN.md)`)
    if (s.n > 1) { L(`⏺ Read(${run}/FEEDBACK.md)`); L(`  ⎿ ${(fx.findings ?? []).length} findings from Review ${s.n - 1}`, C.grey) }
    L(`⏺ Update(${files[0]})`); L('  ⎿ Added 18 lines, removed 4 lines', C.grey)
    ;(fx.diff ?? []).forEach(([k, line]) => L((k === '+' ? '+ ' : '- ') + line.replace(/\t/g, '    '), k === '+' ? C.green : C.red))
    if (s.state === 'approval') {
      L(''); L('╭─ Bash command ─────────────────────────────', C.red); L('│  ' + test, C.red); L('│  Do you want to proceed?', C.red)
      L('│  ❯ 1. Yes', C.red); L("│    2. Yes, and don't ask again for this command", C.red); L('│    3. No, and tell Claude what to do differently', C.red); L('╰────────────────────────────────────────────', C.red)
    } else {
      L(`⏺ Bash(${test})`); L('  ⎿ ' + (fx.testOut || 'ok'), C.grey)
      if (s.state === 'running') { L(''); L(`✻ Writing IMPLEMENT-${s.n}.md…  (esc to interrupt)`, C.yellow) }
      else { L(`⏺ Write(${run}/IMPLEMENT-${s.n}.md)`); L('  ⎿ status: done', C.grey); L(''); L(`● herd: committed ${s.sha} "[${t.slug}] implement-${s.n}" · Review ${s.n} started automatically`, C.cyan) }
    }
  } else if (s.agent === 'review') {
    const sha = prevSha(t, s.key)
    L(`⏺ Bash(git show --stat ${sha})`); L(`  ⎿ ${files.length} files changed, +41 −9`, C.grey); L(`⏺ Read(${run}/PLAN.md)`)
    if (s.state === 'running') { L(''); L(`✻ Reviewing ${sha}…  (esc to interrupt)`, C.yellow) }
    else { (fx.findings ?? []).forEach((f) => L('  · ' + f, C.grey)); L(`⏺ Write(${run}/REVIEW-${s.n}.md)`); L('  ⎿ verdict: ' + s.verdict, s.verdict === 'approve' ? C.cyan : C.yellow) }
  } else {
    L(`⏺ Write(${run}/PR.md)`); L(`● herd: git push -u origin agent/${t.slug}`, C.cyan); L(`● herd: gh pr create --body-file PR.md  →  #${t.pr}`, C.cyan)
    L(`● herd: Copilot review, ${(fx.copilot ?? []).length} comments appended to FEEDBACK.md`, C.cyan)
  }
  return out.join('\r\n') + '\r\n'
}

function mockArtifact(stepId: string): Artifact {
  const [tid, key] = [stepId.slice(0, stepId.lastIndexOf('/')), stepId.slice(stepId.lastIndexOf('/') + 1)]
  const t = find(tid), s = t.steps.find((x) => x.key === key)!, a = agent(s.agent), fx = t.fx
  const file = (a.preview || a.writes).replace('{n}', String(s.n))
  const base = { path: `~/.herd/runs/${proj(t.project).name}/${t.slug}/${file}`, file }
  if (s.state !== 'done') return { ...base, exists: false, front: null, body: '' }
  const list = (x: string[]) => x.map((i) => '- ' + i).join('\n')
  if (s.agent === 'plan') {
    const flow = fx.flow?.length ? fx.flow : ['Read source', 'Change code', 'Test']
    const mer = flow.map((n, i) => `  n${i}${n.endsWith('?') ? `{"${n}"}` : `["${n}"]`}`).join('\n') + '\n' + flow.slice(1).map((_, i) => `  n${i} --> n${i + 1}`).join('\n')
    return { ...base, exists: true, front: [{ k: 'status', v: 'done' }, { k: 'source', v: t.issue ? (t.description ? 'issue+description' : 'issue') : 'local' }],
      body: `## Source\n\n${derive(t).source}\n\n## Goal\n\n${fx.goal ?? t.description ?? t.title}\n\n## Approach\n\n\`\`\`mermaid\nflowchart TD\n${mer}\n\`\`\`\n\n## Files\n\n${list(fx.files ?? ['README.md'])}\n` + (s.guide ? `\n## Guidance\n\n${s.guide}\n` : '') }
  }
  if (s.agent === 'impl') return { ...base, exists: true, front: [{ k: 'status', v: 'done' }, { k: 'commit', v: s.sha ?? '' }],
    body: `## Changes\n\n${list(fx.changes?.length ? fx.changes : ['Implemented the plan'])}\n\n## Tests\n\n\`\`\`console\n$ ${fx.test ?? 'make test'}\n${fx.testOut ?? 'ok'}\n\`\`\`\n\n## Notes for the reviewer\n\nBehaviour outside the files listed in PLAN.md is unchanged.\n` }
  if (s.agent === 'review') return { ...base, exists: true, front: [{ k: 'verdict', v: s.verdict ?? '' }, { k: 'reviewed', v: prevSha(t, s.key) }],
    body: `## Findings\n\n${list(fx.findings?.length ? fx.findings : ['No blocking findings'])}\n\n## Verdict\n\n${s.verdict === 'approve' ? 'Ready to push.' : 'Address the findings above in the next implement step.'}\n` }
  return { ...base, exists: true, front: [{ k: 'pr', v: '#' + t.pr }], body: `## Copilot review (PR #${t.pr})\n\n${list(fx.copilot ?? ['No comments'])}\n` }
}

// ---- the mock service ----

const ok = <T>(v: T) => Promise.resolve(v)

export const api: Api = {
  Bootstrap: () => ok({
    settings, tasks: T.map(derive), os: navigator.platform.toLowerCase().includes('mac') ? 'darwin' : 'linux',
    tabs: { tabs: [{ task: 'core:4356', step: 'core:4356/impl-2' }, { task: 'infra:214', step: 'infra:214/review-1' }, { task: 'rider:1188', step: 'rider:1188/plan-1' }, { task: 'core:4370', step: 'core:4370/impl-1' }], active: 0 },
  }),
  SaveTabs: () => ok(undefined),
  ListTasks: () => ok(T.map(derive)),
  IssueTitle: (_p, issue) => (ISSUE_TITLES[issue] ? ok(ISSUE_TITLES[issue]) : Promise.reject('issue not found')),
  CreateTask: (project, issue, description, runPlan) => {
    if (!issue && !description) return Promise.reject('add a GitHub issue number, a description, or both')
    let slug = issue || 'local-' + description.toLowerCase().replace(/[^a-z0-9 ]/g, ' ').split(/\s+/).filter(Boolean).slice(0, 3).join('-')
    if (T.some((t) => t.project === project && t.slug === slug)) { if (issue) return Promise.reject(`a task for #${issue} already exists`); slug += '-2' }
    const title = issue ? ISSUE_TITLES[issue] ?? 'Issue #' + issue : description.split('\n')[0].replace(/\.$/, '').slice(0, 80)
    const t: MockTask = { id: `${project}:${slug}`, project, issue, slug, title, description, steps: [], fx: { goal: description || title } }
    T.push(t)
    changed(t)
    if (runPlan) api.StartStep(t.id, 'plan', '', 0)
    return ok(derive(t))
  },
  StartStep: (tid, agentId, guidance, n) => {
    const t = find(tid)
    if (!n) n = Math.max(0, ...t.steps.filter((s) => s.agent === agentId).map((s) => s.n)) + 1
    for (const x of t.steps) if (x.manual && x.state !== 'done') x.state = 'done'
    let key = `${agentId}-${n}`
    if (t.steps.some((s) => s.key === key)) key += '-' + (t.steps.length + 1)
    const s: MockStep = { key, agent: agentId, n, state: 'running', guide: guidance, session: Math.random().toString(16).slice(2, 10) }
    t.steps.push(s)
    changed(t)
    later(4000, () => {
      s.state = 'done'
      if (agentId === 'review') s.sha = Math.random().toString(16).slice(2, 9)
      if (agentId === 'review') s.verdict = 'approve'
      if (agentId === 'push') { t.pr = 800 + Math.floor(Math.random() * 99); t.prState = 'open' }
      changed(t)
      emit('artifact:changed', { taskId: t.id, file: '' })
      const auto = agents.find((a) => a.trigger === 'after:' + agentId)
      if (auto) api.StartStep(t.id, auto.id, '', 0)
    })
    return ok(derive(t).steps.find((x) => x.key === key)!)
  },
  StartManualStep: (tid, agentId, guidance, n) => {
    const t = find(tid)
    if (!n) n = Math.max(0, ...t.steps.filter((s) => s.agent === agentId).map((s) => s.n)) + 1
    for (const x of t.steps) if (x.manual && x.state !== 'done') x.state = 'done'
    const s: MockStep = { key: `${agentId}-${n}`, agent: agentId, n, state: 'idle', guide: guidance, session: crypto.randomUUID(), manual: true }
    t.steps.push(s)
    changed(t)
    return ok(derive(t).steps.find((x) => x.key === s.key)!)
  },
  RerunStep: (sid) => {
    const t = find(sid.slice(0, sid.lastIndexOf('/'))), s = t.steps.find((x) => `${t.id}/${x.key}` === sid)
    if (!s) return Promise.reject('no such step')
    s.state = 'running'
    changed(t)
    later(3000, () => { s.state = 'done'; s.sha = Math.random().toString(16).slice(2, 9); changed(t) })
    return ok(undefined)
  },
  AllowOnce: (sid) => { const t = find(sid.slice(0, sid.lastIndexOf('/'))); t.steps.forEach((s) => { if (s.state === 'approval') s.state = 'running' }); changed(t); return ok(undefined) },
  MarkDone: (sid) => { const t = find(sid.slice(0, sid.lastIndexOf('/'))); const s = t.steps.find((x) => `${t.id}/${x.key}` === sid); if (s) s.state = 'done'; changed(t); return ok(undefined) },
  StopStep: () => ok(undefined),
  ArchiveTask: (id) => { const t = find(id); t.archived = true; t.archivedOn = new Date().toISOString(); changed(t); return ok(undefined) },
  RestoreTask: (id) => { const t = find(id); if (t.removed) return Promise.reject('the worktree was removed'); t.archived = false; changed(t); return ok(undefined) },
  RemoveWorktree: (id) => { const t = find(id), p = proj(t.project); t.removed = true; changed(t)
    return ok([`$ git -C ${p.path} worktree remove --force ${p.path}/.worktrees/${t.slug}`, `$ git -C ${p.path} branch -D agent/${t.slug}`]) },
  OpenInZed: (id, sid) => {
    const t = find(id), p = proj(t.project), s = t.steps.find((x) => `${t.id}/${x.key}` === sid)
    if (s?.sha) return ok([`$ zed zed://git/commit/${s.sha}?repo=${p.path}/.worktrees/${t.slug}`])
    return ok([`$ zed ${p.path}/.worktrees/${t.slug}`])
  },
  OpenProjectInZed: () => ok(undefined),
  ReadArtifact: (sid) => ok(mockArtifact(sid)),
  TerminalURL: (sid) => ok('mock:' + sid),
  ResumeStep: (sid) => (new URLSearchParams(location.search).get('elsewhere') ? Promise.reject('Claude Code is still running this session in another process') : ok('mock:' + sid)),
  ResumeStepAnyway: (sid) => ok('mock:' + sid),
  SessionElsewhere: (sid) => {
    if (!new URLSearchParams(location.search).get('elsewhere')) return ok([])
    const t = find(sid.slice(0, sid.lastIndexOf('/'))), s = t.steps.find((x) => `${t.id}/${x.key}` === sid)
    return ok([{ pid: 48213, cmd: `claude --resume ${s?.session ?? ''}` }])
  },
  StepChanges: (sid) => {
    const t = find(sid.slice(0, sid.lastIndexOf('/'))), s = t.steps.find((x) => `${t.id}/${x.key}` === sid)
    const i = s ? t.steps.indexOf(s) : -1, last = i === t.steps.length - 1
    const next = t.steps[i + 1], via = !s?.sha && next?.sha ? next : undefined
    const sha = s?.sha || via?.sha
    if (sha) return ok({ source: 'commit', sha, via: via ? displayName(agent(via.agent), via.n) : '', files: [
      { path: 'internal/unlock/ble.go', status: 'M', added: 42, deleted: 9, binary: false },
      { path: 'internal/unlock/retry.go', status: 'A', added: 61, deleted: 0, binary: false },
      { path: 'internal/unlock/retry_test.go', status: 'A', added: 88, deleted: 0, binary: false },
      { path: 'config/defaults.yaml', status: 'M', added: 2, deleted: 1, binary: false },
    ] })
    if (last && s) return ok({ source: 'worktree', sha: '', via: '', files: [
      { path: 'internal/unlock/ble.go', status: 'M', added: 18, deleted: 4, binary: false },
      { path: 'internal/unlock/budget.go', status: '?', added: 27, deleted: 0, binary: false },
    ] })
    return ok({ source: '', sha: '', via: '', files: [] })
  },
  OpenFileInZed: () => ok(undefined),
  SetFocused: () => ok(undefined),
  SetNotifications: (on) => { settings.config.notifications = on; return ok(undefined) },
  SetMaxTabs: (n) => { settings.config.maxTabs = n; emit('settings:changed', settings); return ok(undefined) },
  SetRules: (r) => { settings.config.rules = r; return ok(undefined) },
  ChooseFolder: () => ok('~/notes/pony-kb'),
  GetSettings: () => ok(settings),
  SaveAgents: (a) => { settings.agents = a; agents.splice(0, agents.length, ...a); emit('settings:changed', settings); T.forEach(changed); return ok(undefined) },
  SaveContext: (c) => { settings.context[c.id] = c; emit('settings:changed', settings); return ok(undefined) },
  SaveProject: (p) => { const i = projects.findIndex((x) => x.id === p.id); if (i >= 0) projects[i] = p; else projects.push(p); emit('settings:changed', settings); return ok(undefined) },
  RemoveProject: (id) => { const i = projects.findIndex((x) => x.id === id); if (i >= 0) projects.splice(i, 1); emit('settings:changed', settings); return ok(undefined) },
  AddProject: () => Promise.reject('The folder picker needs the desktop app'),
  AddProjectPath: () => Promise.reject('Adding projects needs the desktop app'),
  SetTheme: (theme) => { settings.config.appearance.theme = theme as any; emit('settings:changed', settings); return ok(undefined) },
  SaveAppearance: (a, z) => { settings.config.appearance = a; settings.config.zedCommand = z; emit('settings:changed', settings); return ok(undefined) },
  CheckPrerequisites: () => ok([
    { project: '', ok: true, label: 'Zed CLI', detail: '"zed" on PATH' },
    { project: '', ok: true, label: 'GitHub CLI', detail: 'gh authenticated' },
    ...projects.map((p) => ({ project: p.id, ok: true, label: 'Worktrees excluded', detail: '.worktrees/ in .git/info/exclude' })),
  ]),
}
