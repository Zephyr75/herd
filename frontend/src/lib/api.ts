// The Go App service, bound by Wails at window.go.app.App, and its events
// at window.runtime. In a plain browser (npm run dev) a mock built from the
// UI preview's fixtures stands in, so the UI can be worked on without Go.

import type { Agent, Artifact, Bootstrap, Check, Context, Project, Settings, Step, Task, Appearance } from './types'

export interface Api {
  Bootstrap(): Promise<Bootstrap>
  SaveTabs(json: string): Promise<void>
  ListTasks(): Promise<Task[]>
  IssueTitle(project: string, issue: string): Promise<string>
  CreateTask(project: string, issue: string, description: string, runPlan: boolean): Promise<Task>
  StartStep(task: string, agent: string, guidance: string, n: number): Promise<Step>
  AllowOnce(step: string): Promise<void>
  MarkDone(step: string): Promise<void>
  StopStep(step: string): Promise<void>
  ArchiveTask(task: string): Promise<void>
  RestoreTask(task: string): Promise<void>
  RemoveWorktree(task: string): Promise<string[]>
  OpenInZed(task: string, step: string): Promise<string[]>
  OpenProjectInZed(project: string): Promise<void>
  ReadArtifact(step: string): Promise<Artifact>
  TerminalURL(step: string): Promise<string>
  GetSettings(): Promise<Settings>
  SaveAgents(agents: Agent[]): Promise<void>
  SaveContext(c: Context): Promise<void>
  SaveProject(p: Project): Promise<void>
  RemoveProject(id: string): Promise<void>
  AddProject(): Promise<Project | null>
  AddProjectPath(dir: string): Promise<Project | null>
  SetTheme(theme: string): Promise<void>
  SaveAppearance(a: Appearance, zedCommand: string): Promise<void>
  CheckPrerequisites(): Promise<Check[]>
}

type Handler = (data: any) => void

const w = window as any
export const isDesktop = !!w.go?.app?.App

let impl: Api
let subscribe: (event: string, fn: Handler) => () => void

if (isDesktop) {
  impl = new Proxy({} as Api, {
    get: (_t, name: string) => (...args: unknown[]) => w.go.app.App[name](...args),
  })
  subscribe = (event, fn) => w.runtime.EventsOn(event, fn)
} else {
  const mock = await import('./mock')
  impl = mock.api
  subscribe = mock.on
}

export const api: Api = impl

/** on subscribes to a Go event; it returns the unsubscribe function. */
export function on(event: string, fn: Handler): () => void {
  return subscribe(event, fn)
}

/** errorText turns a rejected binding call into a message. */
export function errorText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}
