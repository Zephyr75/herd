// App state: tasks, settings, tabs and what is selected. Components read it
// and call its methods; Go events keep it current. The instance is wrapped
// in Vue's reactive(), so fields are reactive and getters are derived values.

import { reactive } from 'vue'
import { api, errorText, isDesktop, on } from './api'
import { actionFor } from './keymap'
import { copyText } from './window'
import type { Option, Proc, Settings, Step, TabRef, Task } from './types'

export type Overlay = 'none' | 'palette' | 'scope' | 'new' | 'activity'
export type ScopeMode = 'project' | 'worktree' | 'branch'
export type SettingsTab = 'agents' | 'context' | 'projects' | 'appearance' | 'shortcuts'

export interface Toast {
  id: number
  title: string
  body: string
  error: boolean
}

let toastSeq = 0

/** Panel widths: defaults, limits, and the per-viewer saved value. */
export const PANELS = {
  left: { def: 300, min: 220, max: 560 },
  right: { def: 400, min: 300, max: 760 },
} as const
export type PanelSide = keyof typeof PANELS

function savedWidth(side: PanelSide): number {
  try {
    const v = Number(localStorage.getItem('herd.panel.' + side))
    if (v >= PANELS[side].min && v <= PANELS[side].max) return v
  } catch {
    /* storage unavailable */
  }
  return PANELS[side].def
}

function savedNumber(key: string): number {
  try {
    return Number(localStorage.getItem(key)) || 0
  } catch {
    return 0
  }
}

class Herd {
  ready = false
  failed = ''
  os = 'linux'
  settings: Settings | null = null
  tasks: Task[] = []
  tabs: TabRef[] = []
  active = -1
  sel = ''
  left: 'tasks' | 'archive' = 'tasks'
  view: 'board' | 'settings' = 'board'
  settingsTab: SettingsTab = 'agents'
  overlay: Overlay = 'none'
  rightTab: 'preview' | 'changes' = 'preview'
  /** Steps whose session another process still runs (the resume guard). */
  elsewhere: Record<string, Proc[]> = {}
  scope: ScopeMode = 'project'
  collapsed: Record<string, boolean> = {}
  guidance = ''
  toasts: Toast[] = []
  systemLight = false
  artifactRev = 0
  confirmRemove = ''
  leftWidth = savedWidth('left')
  rightWidth = savedWidth('right')
  /** Height of the steps list in the right panel; 0 lets it size itself. */
  timelineHeight = savedNumber('herd.panel.timeline')
  /** Bumped to reconnect the terminal, e.g. after a rerun. */
  termRev = 0

  get projects() {
    return this.settings?.config.projects ?? []
  }
  get selTask(): Task | null {
    return this.tasks.find((t) => t.id === this.sel) ?? null
  }
  get activeTab(): TabRef | null {
    const tb = this.active >= 0 ? this.tabs[this.active] : undefined
    return tb && tb.task === this.sel ? tb : null
  }
  get selStep(): Step | null {
    const tb = this.activeTab
    if (!tb || !this.selTask) return null
    return this.selTask.steps.find((s) => s.id === tb.step) ?? null
  }
  get themeMode() {
    return this.settings?.config.appearance.theme ?? 'dark'
  }
  get theme() {
    return this.themeMode === 'light' || (this.themeMode === 'system' && this.systemLight) ? 'flatwhite' : 'fleet-dark'
  }

  task(id: string) {
    return this.tasks.find((t) => t.id === id)
  }

  project(id: string) {
    return this.projects.find((p) => p.id === id)
  }

  // ---- startup and events ----

  async init() {
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    this.systemLight = mq.matches
    mq.addEventListener('change', (e) => (this.systemLight = e.matches))
    try {
      const b = await api.Bootstrap()
      this.os = b.os
      this.settings = b.settings
      this.tasks = b.tasks ?? []
      const saved = b.tabs?.tabs ?? []
      this.tabs = saved.filter((tb) => this.task(tb.task)?.steps.some((s) => s.id === tb.step))
      this.active = Math.min(b.tabs?.active ?? -1, this.tabs.length - 1)
      if (this.active < 0 && this.tabs.length) this.active = 0
      this.sel = this.tabs[this.active]?.task ?? this.tasks.find((t) => !t.archived)?.id ?? this.tasks[0]?.id ?? ''
      if (this.task(this.sel)?.archived) this.left = 'archive'
      this.trimTabs()
    } catch (e) {
      this.failed = errorText(e)
      return
    }
    if (!isDesktop) this.applyPreviewParams()
    on('task:changed', (t: Task) => this.upsert(t))
    on('settings:changed', (s: Settings) => {
      // While the user is typing in Settings, the local copy is newer than
      // the echo of the previous save.
      if (Date.now() - this.lastEdit > 1500) this.settings = s
    })
    on('artifact:changed', (d: { taskId: string }) => {
      if (d.taskId === this.sel) this.artifactRev++
    })
    on('toast', (d: { title: string; body: string; error: boolean }) => this.toast(d.title, d.body, d.error))
    on('menu', (what: string) => this.command(what))
    if (isDesktop) {
      // Desktop notifications are only shown while herd is in the background.
      const report = () => void api.SetFocused(document.hasFocus() && !document.hidden)
      window.addEventListener('focus', report)
      window.addEventListener('blur', report)
      document.addEventListener('visibilitychange', report)
      report()
    }
    window.addEventListener('keydown', (e) => this.onKey(e), true)
    this.ready = true
  }

  /** In the browser mock, URL params pick a screen like the preview's
   *  artboards: ?sel=core:4349&left=archive&overlay=palette&q=unlock&
   *  view=settings&tab=appearance&theme=light&scope=branch&step=plan-1 */
  private applyPreviewParams() {
    const q = new URLSearchParams(location.search)
    const sel = q.get('sel')
    if (sel && this.task(sel)) {
      const step = q.get('step')
      if (step) this.openStep(sel, `${sel}/${step}`)
      else this.pickTask(sel)
    }
    const left = q.get('left')
    if (left === 'archive' || left === 'tasks') this.left = left
    const theme = q.get('theme')
    if (theme && this.settings && (theme === 'dark' || theme === 'light' || theme === 'system')) this.settings.config.appearance.theme = theme
    if (q.get('view') === 'settings') {
      this.view = 'settings'
      this.settingsTab = (q.get('tab') as SettingsTab) || 'agents'
    }
    const scope = q.get('scope')
    if (scope === 'project' || scope === 'worktree' || scope === 'branch') this.scope = scope
    const overlay = q.get('overlay')
    if (overlay === 'palette' || overlay === 'scope' || overlay === 'new' || overlay === 'activity') this.overlay = overlay
    if (q.get('right') === 'changes') this.rightTab = 'changes'
    this.previewQuery = q.get('q') ?? ''
    this.previewNew = q.get('new') ?? ''
  }
  previewQuery = ''
  previewNew = ''

  private upsert(t: Task) {
    const i = this.tasks.findIndex((x) => x.id === t.id)
    const before = i >= 0 ? this.tasks[i] : null
    if (i >= 0) this.tasks[i] = t
    else this.tasks.push(t)
    if (!before) return
    // A step that started by itself (automatic agents) opens a tab, and takes
    // focus when the user was watching the step before it.
    const known = new Set(before.steps.map((s) => s.id))
    for (const s of t.steps) {
      if (known.has(s.id) || !s.auto) continue
      if (this.tabs.some((tb) => tb.step === s.id)) continue
      const prev = before.steps[before.steps.length - 1]
      const watching = this.activeTab?.step === prev?.id
      const hasTab = this.tabs.some((tb) => tb.task === t.id)
      if (!hasTab) continue
      this.tabs.push({ task: t.id, step: s.id })
      if (watching) this.active = this.tabs.length - 1
      this.trimTabs()
      this.persistTabs()
    }
  }

  toast(title: string, body = '', error = false) {
    const id = ++toastSeq
    this.toasts.push({ id, title, body, error })
    setTimeout(() => (this.toasts = this.toasts.filter((x) => x.id !== id)), error ? 9000 : 5000)
  }

  async attempt<T>(title: string, fn: () => Promise<T>): Promise<T | undefined> {
    try {
      return await fn()
    } catch (e) {
      this.toast(title, errorText(e), true)
      return undefined
    }
  }

  /** setPanelWidth resizes a side panel (clamped) and remembers it. */
  setPanelWidth(side: PanelSide, w: number) {
    const { min, max } = PANELS[side]
    // Keep at least 360px for the terminal between the panels.
    const other = side === 'left' ? this.rightWidth : this.leftWidth
    const room = window.innerWidth - other - 360
    const v = Math.round(Math.max(min, Math.min(max, room, w)))
    if (side === 'left') this.leftWidth = v
    else this.rightWidth = v
    try {
      localStorage.setItem('herd.panel.' + side, String(v))
    } catch {
      /* storage unavailable */
    }
  }

  setTimelineHeight(h: number) {
    this.timelineHeight = Math.max(0, Math.round(h))
    try {
      localStorage.setItem('herd.panel.timeline', String(this.timelineHeight))
    } catch {
      /* storage unavailable */
    }
  }

  /** edited marks a local settings edit (see settings:changed). */
  lastEdit = 0
  edited() {
    this.lastEdit = Date.now()
  }

  private persistTimer = 0
  persistTabs() {
    clearTimeout(this.persistTimer)
    this.persistTimer = window.setTimeout(() => {
      void api.SaveTabs(JSON.stringify({ tabs: this.tabs, active: this.active })).catch(() => {})
    }, 300)
  }

  // ---- selection and tabs ----

  /** When each tab was last opened or focused, by step id. */
  private tabUsed: Record<string, number> = {}

  /** trimTabs closes the least recently used tabs above the limit in Settings. */
  trimTabs() {
    const max = Math.max(1, this.settings?.config.maxTabs || 5)
    const keep = this.tabs[this.active]
    if (keep) this.tabUsed[keep.step] = Date.now()
    while (this.tabs.length > max) {
      let old = -1
      this.tabs.forEach((tb, i) => {
        if (tb !== keep && (old < 0 || (this.tabUsed[tb.step] ?? 0) < (this.tabUsed[this.tabs[old].step] ?? 0))) old = i
      })
      if (old < 0) break
      delete this.tabUsed[this.tabs[old].step]
      this.tabs.splice(old, 1)
      if (old < this.active) this.active--
    }
    this.persistTabs()
  }

  openStep(taskId: string, stepId: string) {
    let ix = this.tabs.findIndex((tb) => tb.task === taskId && tb.step === stepId)
    if (ix < 0) {
      this.tabs.push({ task: taskId, step: stepId })
      ix = this.tabs.length - 1
    }
    this.active = ix
    this.trimTabs()
    this.sel = taskId
    this.overlay = 'none'
    this.view = 'board'
    const t = this.task(taskId)
    if (t) this.left = t.archived ? 'archive' : 'tasks'
    this.persistTabs()
  }

  pickTask(id: string) {
    const t = this.task(id)
    if (!t) return
    this.overlay = 'none'
    this.view = 'board'
    this.left = t.archived ? 'archive' : 'tasks'
    this.confirmRemove = ''
    if (!t.steps.length) {
      this.sel = id
      this.active = -1
      return
    }
    let last = -1
    this.tabs.forEach((tb, i) => {
      if (tb.task === id) last = i
    })
    if (last >= 0) {
      this.active = last
      this.sel = id
      this.persistTabs()
    } else this.openStep(id, t.steps[t.steps.length - 1].id)
  }

  focusTab(i: number) {
    const tb = this.tabs[i]
    if (!tb) return
    this.active = i
    this.tabUsed[tb.step] = Date.now()
    this.sel = tb.task
    this.view = 'board'
    const t = this.task(tb.task)
    if (t?.archived) this.left = 'archive'
    else if (t) this.left = 'tasks'
    this.persistTabs()
  }

  closeTab(i: number) {
    this.tabs.splice(i, 1)
    let a = this.active
    if (i === a) a = Math.min(i, this.tabs.length - 1)
    else if (i < a) a--
    this.active = a
    if (a >= 0 && this.tabs[a]) this.sel = this.tabs[a].task
    this.persistTabs()
  }

  /** The tasks in the left panel, in display order. */
  visibleTasks(): Task[] {
    const out: Task[] = []
    for (const p of this.projects) {
      if (this.collapsed[p.id]) continue
      out.push(...this.tasks.filter((t) => t.project === p.id && (this.left === 'archive' ? t.archived : !t.archived)))
    }
    return out
  }

  moveTask(delta: number) {
    const list = this.visibleTasks()
    if (!list.length) return
    const i = list.findIndex((t) => t.id === this.sel)
    const next = list[Math.max(0, Math.min(list.length - 1, i < 0 ? 0 : i + delta))]
    this.pickTask(next.id)
  }

  // ---- actions on the selected task ----

  /** runManual creates a step without running it and copies the command
   *  to use its session outside herd. */
  async runManual(opt: Option, taskId = this.sel) {
    const guidance = this.guidance
    const step = await this.attempt('Could not create ' + opt.label, () => api.StartManualStep(taskId, opt.agent, guidance, opt.n))
    if (!step) return
    this.guidance = ''
    this.openStep(taskId, step.id)
    await this.copyResume(step)
  }

  /** rerunStep runs a finished step again in its own session. */
  async rerunStep(step: Step) {
    const ok = await this.attempt('Could not rerun ' + step.name, () => api.RerunStep(step.id).then(() => true))
    if (!ok) return
    this.termRev++
    this.openStep(step.taskId, step.id)
  }

  async run(opt: Option, taskId = this.sel) {
    const guidance = this.guidance
    const step = await this.attempt('Could not start ' + opt.label, () => api.StartStep(taskId, opt.agent, guidance, opt.n))
    if (!step) return
    this.guidance = ''
    this.openStep(taskId, step.id)
  }

  runPrimary() {
    const t = this.selTask
    const opt = t?.next.options[0]
    if (t && opt && !t.archived) void this.run(opt)
  }

  async openWorktree() {
    const t = this.selTask
    if (!t) return
    const lines = await this.attempt('Could not open Zed', () => api.OpenInZed(t.id, ''))
    if (lines) this.toast('Opening worktree in Zed', lines.join('\n'))
  }

  async openDiff(step: Step | null = this.selStep) {
    const t = this.selTask
    if (!t || !step) return
    const lines = await this.attempt('Could not open the diff', () => api.OpenInZed(t.id, step.sha ? step.id : ''))
    if (lines) this.toast(step.sha ? 'Opening step diff in Zed' : 'Opening worktree in Zed', lines.join('\n'))
  }

  // ---- a step's Claude Code session, e.g. to continue it in Zed ----

  async copyResume(step: Step) {
    const cmd = resumeCommand(step)
    if (await copyText(cmd)) this.toast('Copied', cmd)
    else this.toast('Could not copy', cmd, true)
  }

  stopStep(step: Step) {
    return this.attempt('Could not stop Claude', () => api.StopStep(step.id))
  }

  /** Resume, unless another process (e.g. claude --resume in Zed) still
   *  runs the session: then the terminal shows who, and Resume anyway. */
  async resumeStep(step: Step) {
    try {
      await api.ResumeStep(step.id)
      delete this.elsewhere[step.id]
    } catch (e) {
      const procs = await api.SessionElsewhere(step.id).catch(() => [])
      if (procs.length) this.elsewhere[step.id] = procs
      else this.toast('Could not resume', errorText(e), true)
    }
  }

  async resumeAnyway(step: Step) {
    delete this.elsewhere[step.id]
    await this.attempt('Could not resume', () => api.ResumeStepAnyway(step.id))
  }

  // ---- overlays and commands ----

  openOverlay(o: Overlay, scope?: ScopeMode) {
    if (scope) this.scope = scope
    this.overlay = o
  }

  command(action: string) {
    if (action.startsWith('task:')) {
      // A click on a desktop notification.
      this.overlay = 'none'
      this.view = 'board'
      return this.pickTask(action.slice(5))
    }
    const board = this.view === 'board'
    switch (action) {
      case 'palette':
        return this.openOverlay(this.overlay === 'palette' ? 'none' : 'palette')
      case 'newTask':
        return this.openOverlay('new')
      case 'settings':
        this.overlay = 'none'
        this.view = this.view === 'settings' ? 'board' : 'settings'
        return
      case 'close':
        if (this.overlay !== 'none') this.overlay = 'none'
        else if (this.view === 'settings') this.view = 'board'
        return
    }
    if (!board || this.overlay !== 'none') return
    switch (action) {
      case 'scope':
        return this.openOverlay('scope', 'project')
      case 'runPrimary':
        return this.runPrimary()
      case 'toggleArchive':
        this.left = this.left === 'tasks' ? 'archive' : 'tasks'
        return
      case 'nextTask':
        return this.moveTask(1)
      case 'prevTask':
        return this.moveTask(-1)
      case 'closeTab':
        if (this.activeTab) this.closeTab(this.active)
        return
      case 'nextTab':
      case 'prevTab':
        if (this.tabs.length) this.focusTab((Math.max(this.active, 0) + (action === 'nextTab' ? 1 : this.tabs.length - 1)) % this.tabs.length)
        return
      case 'openWorktree':
        return void this.openWorktree()
      case 'openDiff':
        return void this.openDiff()
    }
    const m = /^tab([1-9])$/.exec(action)
    if (m) this.focusTab(Number(m[1]) - 1)
  }

  private onKey(e: KeyboardEvent) {
    if (!this.settings) return
    const action = actionFor(this.settings.keymap, e)
    if (!action) return
    // ⌘⏎ inside a popup belongs to the popup (Create and run Plan, open in Zed).
    if (this.overlay !== 'none' && action === 'runPrimary') return
    // Escape with nothing to close belongs to the focused element, e.g. Claude Code in the terminal.
    if (action === 'close' && this.overlay === 'none' && this.view === 'board') return
    e.preventDefault()
    e.stopPropagation()
    this.command(action)
  }
}

/** resumeCommand continues a step's session elsewhere; a manual step nobody
 *  used yet has no transcript, so it starts the session with its id. */
export const resumeCommand = (step: Step) => (step.fresh ? `claude --session-id ${step.session}` : `claude --resume ${step.session}`)

export const herd = reactive(new Herd())
