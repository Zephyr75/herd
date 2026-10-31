// Shapes returned by the Go App service (internal/app, internal/settings).

export type StepState = 'running' | 'idle' | 'approval' | 'done' | 'failed'
export type StatusKind = 'running' | 'waiting' | 'approval' | 'idle' | 'done'

export interface Project {
  id: string
  name: string
  path: string
  repo: string
  baseBranch: string
  worktreesDir: string
  portRange: string
}

export interface Agent {
  id: string
  name: string
  command: string
  model: string
  trigger: string // manual | after:<id>
  writes: string
  preview: string
  done: string
  permissions: string // manual | acceptEdits | readonly
  allowed: string[]
  denied: string[]
  commit: boolean
  push: boolean
  instructions: string
}

export interface Context {
  id: string
  text: string
  agents: string[]
  claudeMd: boolean
}

export interface Appearance {
  theme: 'dark' | 'light' | 'system'
  uiFont: string
  uiFontSize: number
  termFont: string
  termFontSize: number
}

export interface Config {
  appearance: Appearance
  zedCommand: string
  claudeCommand: string
  maxLiveSessions: number
  copilotTimeoutMinutes: number
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
  preview: string
  commits: boolean
}

export interface Option {
  agent: string
  n: number
  label: string
}

export interface Next {
  options: Option[]
  text: string
  approval: string
  markDone: string
  ghost: { name: string; after: string } | null
}

export interface Task {
  id: string
  project: string
  slug: string
  issue: string
  title: string
  description: string
  portBase: number
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
}

export interface Field {
  k: string
  v: string
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

export interface Bootstrap {
  settings: Settings
  tasks: Task[]
  tabs: { tabs: TabRef[]; active: number } | null
  os: string
}
