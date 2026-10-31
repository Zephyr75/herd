// Keyboard shortcuts from ~/.herd/keymap.json. "Mod" is ⌘ on macOS and
// Ctrl elsewhere.

import type { Keymap } from './types'

export const isMac = () => /mac/i.test(navigator.platform) || /Mac OS X/.test(navigator.userAgent)

interface Combo {
  mod: boolean
  shift: boolean
  alt: boolean
  ctrl: boolean
  key: string
}

function parse(s: string): Combo {
  const parts = s.split('+')
  // "Mod++" would be a plus key; not used by default.
  const key = parts.pop() ?? ''
  const c: Combo = { mod: false, shift: false, alt: false, ctrl: false, key: key.length === 1 ? key.toLowerCase() : key }
  for (const p of parts) {
    const m = p.toLowerCase()
    if (m === 'mod' || m === 'cmd' || m === 'meta') c.mod = true
    else if (m === 'shift') c.shift = true
    else if (m === 'alt' || m === 'option') c.alt = true
    else if (m === 'ctrl' || m === 'control') c.ctrl = true
  }
  return c
}

function keyOf(e: KeyboardEvent): string {
  if (e.key.length === 1) return e.key.toLowerCase()
  return e.key
}

function matches(c: Combo, e: KeyboardEvent, mac: boolean): boolean {
  const wantMeta = mac ? c.mod : false
  const wantCtrl = mac ? c.ctrl : c.mod || c.ctrl
  if (e.metaKey !== wantMeta || e.ctrlKey !== wantCtrl || e.altKey !== c.alt || e.shiftKey !== c.shift) return false
  const k = keyOf(e)
  if (k === c.key) return true
  // Shifted or alt-modified keys report another character: fall back to the physical key.
  if (c.key.length === 1 && /^[a-z0-9]$/.test(c.key)) {
    return e.code === 'Key' + c.key.toUpperCase() || e.code === 'Digit' + c.key
  }
  return c.key === ',' && e.code === 'Comma'
}

/** actionFor returns the keymap action an event triggers, if any. */
export function actionFor(km: Keymap, e: KeyboardEvent): string | null {
  const mac = isMac()
  for (const [action, bindings] of Object.entries(km)) {
    for (const b of bindings) if (matches(parse(b), e, mac)) return action
  }
  return null
}

const glyph: Record<string, string> = { ArrowDown: '↓', ArrowUp: '↑', ArrowLeft: '←', ArrowRight: '→', Enter: '⏎', Escape: 'Esc' }

/** label renders a binding for a <kbd>: ⌘⇧A on macOS, Ctrl+Shift+A elsewhere. */
export function label(binding: string | undefined): string {
  if (!binding) return ''
  const c = parse(binding)
  const key = glyph[c.key] ?? (c.key.length === 1 ? c.key.toUpperCase() : c.key)
  if (isMac()) return (c.ctrl ? '⌃' : '') + (c.alt ? '⌥' : '') + (c.shift ? '⇧' : '') + (c.mod ? '⌘' : '') + key
  const mods = [c.mod || c.ctrl ? 'Ctrl' : '', c.alt ? 'Alt' : '', c.shift ? 'Shift' : ''].filter(Boolean)
  return [...mods, key].join('+')
}

/** first returns the label of an action's first binding. */
export const actions: { id: string; action: string; where: string }[] = [
  { id: 'palette', action: 'Go to task (all projects)', where: 'anywhere' },
  { id: 'scope', action: 'Switch task in this project, worktree or branch', where: 'board' },
  { id: 'newTask', action: 'New task', where: 'anywhere' },
  { id: 'runPrimary', action: 'Run the primary next step', where: 'board' },
  { id: 'toggleArchive', action: 'Toggle Tasks and Archive', where: 'board' },
  { id: 'nextTask', action: 'Next task in the list', where: 'board' },
  { id: 'prevTask', action: 'Previous task in the list', where: 'board' },
  { id: 'tab1', action: 'Go to tab 1 to 9', where: 'board' },
  { id: 'closeTab', action: 'Close tab', where: 'board' },
  { id: 'openWorktree', action: 'Open worktree in Zed', where: 'board' },
  { id: 'openDiff', action: 'Open the focused step diff in Zed', where: 'board' },
  { id: 'settings', action: 'Settings', where: 'anywhere' },
  { id: 'close', action: 'Close popup or settings', where: 'popups' },
]
