// Shapes returned by the Go App service (internal/app, internal/settings).

export type StepState = 'running' | 'idle' | 'approval' | 'done' | 'failed'
export type StatusKind = 'running' | 'waiting' | 'approval' | 'idle' | 'done' | 'failed'

export interface Project {
  id: string
  name: string
  path: string
  repo: string
  baseBranch: string
  worktreesDir: string
}

export interface Agent {
  id: string
  name: string
  command: string
  model: string
  trigger: string // manual | after:<id> (when that step succeeds)
  writes: string
  commit: boolean // herd commits when it succeeds: its artifact's title line and body are the message
  push: boolean // then pushes the branch (needs commit)
  openPR: boolean // then opens or updates the draft PR from its artifact (needs push)
  revert: boolean // its latest step can be reverted while nothing is committed after it
  rerun: boolean // its step can be run again while it is the latest
  choose: boolean // when its artifact lists Options, Next step asks which one to take
  ignoreRules: boolean // the common rules are not given to this agent
  showDiff: boolean // its steps have a Diff button (open their changes in Zed)
  sisters: boolean // its artifact can ask for sister tasks in other repositories
  allow: string[] // tool rules it runs without asking, e.g. Bash(make test-path:*)
  next: string[] // agents Next step offers after it succeeds, first highlighted
  onFail: string[] // agents Next step offers after it fails (empty: itself again)
  instructions: string
}

export interface Context {
  id: string
  text: string
  claudeMd: boolean
  knowledge: string // a knowledge base folder sessions can read
}

export interface Appearance {
  theme: 'dark' | 'light' | 'system'
  uiFont: string
  uiFontSize: number
  termFont: string
  termFontSize: number
}

export interface FileChange {
  path: string
  status: string // A added, M modified, D deleted, ? untracked
  added: number
  deleted: number
  binary: boolean
}

export interface Changes {
  source: '' | 'commit' | 'worktree'
  sha: string
  via: string // the step that made the commit, when not this one
  files: FileChange[]
}

/** The user's ~/.claude/CLAUDE.md compared with General context + Rules. */
export interface SystemMemory {
  path: string
  exists: boolean
  synced: boolean
}

/** One file's unified diff in a step's changes. */
export interface FileDiff {
  path: string
  diff: string
  binary: boolean
  truncated: boolean
}

/** A process outside herd running a session. */
export interface Proc {
  pid: number
  cmd: string
}

export interface Config {
  appearance: Appearance
  zedCommand: string
  claudeCommand: string
  maxLiveSessions: number
  notifications: boolean
  maxTabs: number
  rules: string // followed by every agent
  branchPrefix: string // every new task's branch starts with it
  projects: Project[] | null
}

export type Keymap = Record<string, string[]>

export interface Settings {
  config: Config
  agents: Agent[]
  context: Record<string, Context>
  keymap: Keymap
  dir: string
}

export interface Step {
  id: string
  taskId: string
  key: string
  agent: string
  n: number
  seq: number
  state: StepState
  guidance: string
  session: string
  sha: string
  verdict: string
  model: string
  completed: boolean
  startedAt: string
  endedAt: string
  name: string
  live: boolean
  auto: boolean
  artifact: string
  commits: boolean
  diff: boolean // Diff has something to open: its segment's commit, or the uncommitted work
  revert: boolean // after the latest commit: Revert puts the worktree back to its start and removes it and later steps
  paused: boolean // stopped by the user until Resume
  rerun: boolean // Rerun applies: the latest step, of an agent that can be rerun
  gitError: string // why herd's commit, push or PR after it failed
}

export interface Option {
  agent: string
  n: number
  label: string
}

/** A repository the plan also needs; project is '' until it is a herd project. */
export interface SisterOffer {
  repo: string
  scope: string
  project: string
}

export interface Next {
  options: Option[]
  text: string
  approval: string
  markDone: string
  warning: string // e.g. the plan lists options: name one in the guidance
  elsewhere: string[] // before a step that commits: repositories outside the worktree with uncommitted agent edits
  sisters: SisterOffer[] // repositories the plan also needs, with no sister task yet
  ghost: { name: string; after: string } | null
  finished: boolean // the last step succeeded and offers nothing next
}

export interface Task {
  id: string
  project: string
  slug: string
  issue: string
  issueRepo: string // the repository holding the issue, when not the project's (a sister task)
  sisters: { id: string; project: string }[] // tasks for the other repositories of the same work
  title: string
  description: string
  createdAt: string
  archived: boolean
  archivedAt: string
  pr: number
  prUrl: string
  prState: string
  worktreeRemoved: boolean
  projectName: string
  repo: string
  baseBranch: string
  worktree: string
  worktreeRel: string
  branch: string
  runDir: string
  source: string
  status: { kind: StatusKind; label: string; short: string }
  steps: Step[]
  next: Next
  projectGone: boolean // its project was removed from Settings > Projects
  checks: CICheck[] // CI checks of the open PR
  labels: string[] // labels of the open PR
  draft: boolean // the open PR is a draft (herd opens them so)
}


export interface Field {
  k: string
  v: string
}

/** A diagram page (archify HTML) in a task's run directory. */
export interface Diagram {
  file: string
  title: string
  step: string // the step whose artifact embeds it, e.g. Implement 2
  mod: string
}

export interface Artifact {
  path: string
  file: string
  exists: boolean
  front: Field[] | null
  body: string
}

export interface Check {
  project: string
  ok: boolean
  label: string
  detail: string
}

export interface TabRef {
  task: string
  step: string
}

/** One CI check on a PR; bucket is pass, fail, pending, skipping or cancel. */
export interface CICheck {
  name: string
  workflow: string
  bucket: string
  link: string
}

/** An open GitHub issue assigned to you, for New task. */
export interface Issue {
  number: number
  title: string
  priority: string // the issue's Priority field (Urgent, High, …), '' when unset
}

export interface Bootstrap {
  settings: Settings
  tasks: Task[]
  tabs: { tabs: TabRef[]; active: number } | null
  os: string
}

/** afterOf is the agent id a trigger starts after ("after:review" is review), or "". */
export const afterOf = (trigger: string) => (trigger.startsWith('after:') ? trigger.slice(6) : '')

/** unsuccessful: the step finished, and its agent reported status: failed ("changes": older reviews). */
export const unsuccessful = (s: { state: string; verdict: string }) => s.state === 'done' && (s.verdict === 'failed' || s.verdict === 'changes')
