<script setup lang="ts">
// A unified diff: hunks with old and new line numbers. With commentable, a
// click on a line number starts a comment on it (shift-click: on a range);
// the comments show under their last line.
import { computed, nextTick, ref } from 'vue'
import type { Note } from '../lib/types'

const props = defineProps<{ diff: string; notes?: Note[]; commentable?: boolean }>()
const emit = defineEmits<{ add: [n: Omit<Note, 'id' | 'path'>]; update: [id: string, text: string]; remove: [id: string] }>()

interface Row {
  kind: 'hunk' | 'add' | 'del' | 'ctx' | 'note'
  old: number | null
  new: number | null
  text: string
}

const rows = computed(() => {
  const out: Row[] = []
  let o = 0
  let n = 0
  let inHunk = false
  for (const line of props.diff.split('\n')) {
    const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/.exec(line)
    if (m) {
      o = Number(m[1])
      n = Number(m[2])
      inHunk = true
      out.push({ kind: 'hunk', old: null, new: null, text: m[3].trim() })
      continue
    }
    if (!inHunk) continue // diff --git, index, ---, +++ headers
    if (line.startsWith('+')) out.push({ kind: 'add', old: null, new: n++, text: line.slice(1) })
    else if (line.startsWith('-')) out.push({ kind: 'del', old: o++, new: null, text: line.slice(1) })
    else if (line.startsWith('\\')) out.push({ kind: 'note', old: null, new: null, text: line.slice(2) })
    else if (line.startsWith('diff --git')) inHunk = false
    else out.push({ kind: 'ctx', old: o++, new: n++, text: line.slice(1) })
  }
  return out
})

// Selection: the rows a new comment is on.
const anchor = ref(-1)
const end = ref(-1)
const editing = ref('') // a note's id, or 'new'
const draft = ref('')
const box = ref<HTMLTextAreaElement[]>()
const lo = computed(() => Math.min(anchor.value, end.value))
const hi = computed(() => Math.max(anchor.value, end.value))
const selected = (i: number) => anchor.value >= 0 && i >= lo.value && i <= hi.value
const lineRow = (r: Row) => r.kind === 'add' || r.kind === 'del' || r.kind === 'ctx'

function focus() {
  void nextTick(() => box.value?.[0]?.focus())
}
function pick(i: number, e: MouseEvent) {
  if (!props.commentable || !lineRow(rows.value[i])) return
  if (e.shiftKey && anchor.value >= 0 && editing.value === 'new') end.value = i
  else (anchor.value = i), (end.value = i)
  editing.value = 'new'
  focus()
}
function cancel() {
  anchor.value = end.value = -1
  editing.value = ''
  draft.value = ''
}
function save() {
  const text = draft.value.trim()
  if (editing.value === 'new' && text) {
    const sel = rows.value.slice(lo.value, hi.value + 1).filter(lineRow)
    // New line numbers unless every line is a removed one.
    const side = sel.some((r) => r.new !== null) ? 'new' : 'old'
    const nums = sel.map((r) => (side === 'new' ? r.new : r.old)).filter((x): x is number => x !== null)
    const code = sel.map((r) => (r.kind === 'add' ? '+' : r.kind === 'del' ? '-' : ' ') + r.text).join('\n')
    emit('add', { side, from: Math.min(...nums), to: Math.max(...nums), code, text })
  } else if (editing.value && editing.value !== 'new') {
    if (text) emit('update', editing.value, text)
    else emit('remove', editing.value)
  }
  cancel()
}
function edit(n: Note) {
  anchor.value = end.value = -1
  editing.value = n.id
  draft.value = n.text
  focus()
}
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.stopPropagation()
    cancel()
  } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) save()
}

// Each note under the row of its last line; the ones whose lines left the diff at the end.
const placed = computed(() => {
  const at = new Map<number, Note[]>()
  const lost: Note[] = []
  for (const n of props.notes ?? []) {
    let idx = -1
    rows.value.forEach((r, i) => {
      if ((n.side === 'new' ? r.new : r.old) === n.to && (n.side === 'new' || r.kind === 'del')) idx = i
    })
    if (idx < 0) lost.push(n)
    else at.set(idx, [...(at.get(idx) ?? []), n])
  }
  return { at, lost }
})
const span = (n: Note) => (n.from === n.to ? `line ${n.to}` : `lines ${n.from}–${n.to}`) + (n.side === 'old' ? ' (removed)' : '')
</script>

<template>
  <div class="diff" :class="{ commentable }">
    <table>
      <tbody>
        <template v-for="(r, i) in rows" :key="i">
          <tr :class="[r.kind, { sel: selected(i) }]">
            <template v-if="r.kind === 'hunk'">
              <td class="ln" colspan="2">⋯</td>
              <td class="code">{{ r.text }}</td>
            </template>
            <template v-else>
              <td class="ln" :title="commentable && lineRow(r) ? 'Comment on this line (shift-click: a range)' : ''" @click="pick(i, $event)">{{ r.old ?? '' }}</td>
              <td class="ln" :title="commentable && lineRow(r) ? 'Comment on this line (shift-click: a range)' : ''" @click="pick(i, $event)">{{ r.new ?? '' }}</td>
              <td class="code"><span class="sign">{{ r.kind === 'add' ? '+' : r.kind === 'del' ? '−' : ' ' }}</span>{{ r.text }}</td>
            </template>
          </tr>
          <tr v-for="n in placed.at.get(i) ?? []" :key="n.id" class="note-row">
            <td colspan="3">
              <div v-if="editing === n.id" class="editor">
                <textarea ref="box" v-model="draft" rows="3" class="area" data-own-escape @keydown="key"></textarea>
                <div class="bar"><span class="hint">Ctrl+Enter saves · empty removes it</span><button class="b" @click="cancel">Cancel</button><button class="b on" @click="save">Save</button></div>
              </div>
              <div v-else class="note" title="Click to edit" @click="edit(n)">
                <span class="where">{{ span(n) }}</span>
                <span class="text">{{ n.text }}</span>
                <button class="x" aria-label="Remove the comment" title="Remove the comment" @click.stop="emit('remove', n.id)">×</button>
              </div>
            </td>
          </tr>
          <tr v-if="editing === 'new' && i === hi" class="note-row">
            <td colspan="3">
              <div class="editor">
                <textarea ref="box" v-model="draft" rows="3" class="area" data-own-escape placeholder="What should change here?" @keydown="key"></textarea>
                <div class="bar"><span class="hint">Ctrl+Enter saves · shift-click a line number for a range</span><button class="b" @click="cancel">Cancel</button><button class="b on" :disabled="!draft.trim()" @click="save">Comment</button></div>
              </div>
            </td>
          </tr>
        </template>
        <tr v-for="n in placed.lost" :key="n.id" class="note-row">
          <td colspan="3">
            <div class="note" title="Its lines are no longer in this diff" @click="edit(n)">
              <span class="where">{{ span(n) }}, no longer in the diff</span>
              <span class="text">{{ n.text }}</span>
              <button class="x" aria-label="Remove the comment" title="Remove the comment" @click.stop="emit('remove', n.id)">×</button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.diff {
  width: max-content; /* the scrolling parent scrolls long lines sideways */
  min-width: 100%;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.55;
  tab-size: 4;
}
table {
  border-collapse: collapse;
  min-width: 100%;
}
td {
  padding: 0;
  vertical-align: top;
}
.ln {
  width: 1%;
  padding: 0 6px;
  text-align: right;
  color: var(--faint);
  user-select: none;
  white-space: nowrap;
}
.code {
  padding: 0 12px 0 4px;
  white-space: pre;
}
.sign {
  display: inline-block;
  width: 14px;
  color: var(--muted);
  user-select: none;
}
.add {
  background: var(--add-bg);
}
.add .sign {
  color: var(--add);
}
.del {
  background: var(--del-bg);
}
.del .sign {
  color: var(--del);
}
.hunk td {
  padding-top: 4px;
  padding-bottom: 4px;
  background: var(--surface);
  color: var(--muted);
}
.note .code {
  color: var(--muted);
  font-style: italic;
}
.commentable .ln {
  cursor: pointer;
}
.commentable .ln:hover {
  color: var(--text);
}
.sel td {
  box-shadow: inset 0 0 0 9999px color-mix(in srgb, var(--accent) 14%, transparent);
}
.note-row td {
  padding: 4px 12px 6px 60px;
  font-family: var(--ui-font);
  white-space: normal;
}
.note {
  display: flex;
  align-items: baseline;
  gap: 8px;
  max-width: 720px;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  border-radius: 4px;
  background: var(--surface);
  font-size: 12.5px;
  cursor: text;
}
.where {
  flex: none;
  color: var(--muted);
  font-size: 11.5px;
}
.text {
  flex: 1;
  white-space: pre-wrap;
}
.x {
  border: 0;
  background: transparent;
  color: var(--muted);
  font-size: 14px;
  line-height: 1;
}
.x:hover {
  color: var(--text);
}
.editor {
  max-width: 720px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.editor .area {
  width: 100%;
  font-family: var(--ui-font);
  font-size: 12.5px;
}
.bar {
  display: flex;
  align-items: center;
  gap: 6px;
}
.hint {
  flex: 1;
  color: var(--muted);
  font-size: 11.5px;
}
.b {
  height: 24px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
}
.b.on {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.b:disabled {
  opacity: 0.5;
}
</style>
