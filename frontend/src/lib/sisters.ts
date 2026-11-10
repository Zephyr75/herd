// Sister groups: tasks linked as sisters (the same work in several
// repositories), each group with its own color so it can be spotted across
// projects. Tasks without sisters belong to no group.

import { computed } from 'vue'
import { herd } from './state'

export const SISTER_COLORS = 4

export interface SisterGroup {
  color: number // 0..SISTER_COLORS-1
  ids: string[]
}

/** groupOf maps each task with sisters to its group. */
export const groupOf = computed(() => {
  const out = new Map<string, SisterGroup>()
  const byId = new Map(herd.tasks.map((t) => [t.id, t]))
  const keys: string[][] = []
  for (const t of herd.tasks) {
    if (out.has(t.id) || !t.sisters.length) continue
    // Links go both ways, but follow them all in case one side is missing.
    const ids: string[] = []
    const todo = [t.id]
    while (todo.length) {
      const id = todo.pop()!
      if (ids.includes(id)) continue
      ids.push(id)
      for (const x of byId.get(id)?.sisters ?? []) if (byId.has(x.id)) todo.push(x.id)
    }
    if (ids.length < 2) continue
    ids.sort()
    const g = { color: 0, ids }
    for (const id of ids) out.set(id, g)
    keys.push(ids)
  }
  // Colors follow the groups' order (oldest task id first): stable while the groups stay the same.
  keys.sort((a, b) => a[0].localeCompare(b[0])).forEach((ids, i) => (out.get(ids[0])!.color = i % SISTER_COLORS))
  return out
})

/** sisterTitle names a task's sisters, for a tooltip. */
export function sisterTitle(id: string): string {
  const g = groupOf.value.get(id)
  if (!g) return ''
  const names = g.ids.filter((x) => x !== id).map((x) => herd.task(x)?.projectName ?? x)
  return 'Sister tasks in ' + names.join(', ')
}
