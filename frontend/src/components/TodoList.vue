<script setup lang="ts">
// A todo list: tick an item, click its text to edit it (Enter saves,
// Shift+Enter breaks the line, Escape cancels), drag its grip to move it,
// mark it urgent with the warning icon (its text turns red), add one at the bottom.
import { nextTick, ref } from 'vue'
import type { Todo } from '../lib/types'
import Icon from './ui/Icon.vue'

const props = defineProps<{ todos: Todo[]; hideDone?: boolean }>()
const emit = defineEmits<{ change: [todos: Todo[]] }>()

const save = (todos: Todo[]) => emit('change', todos)
const patch = (id: string, p: Partial<Todo>) => save(props.todos.map((x) => (x.id === id ? { ...x, ...p } : x)))
const shown = (t: Todo) => !(props.hideDone && t.done && editing.value !== t.id && dragging.value !== t.id)

// ---- editing ----
const editing = ref('')
const draft = ref('')
const adding = ref('')
const editor = ref<HTMLTextAreaElement[]>([])
async function edit(t: Todo) {
  editing.value = t.id
  draft.value = t.text
  await nextTick()
  const el = editor.value[0]
  if (el) {
    grow(el)
    el.focus()
  }
}
function commit() {
  const id = editing.value
  if (!id) return
  editing.value = ''
  const text = draft.value.trim()
  const t = props.todos.find((x) => x.id === id)
  if (!t || t.text === text) return
  if (text) patch(id, { text })
  else save(props.todos.filter((x) => x.id !== id))
}
function editKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    commit()
  } else if (e.key === 'Escape') {
    e.preventDefault()
    editing.value = ''
  }
}
function add() {
  const text = adding.value.trim()
  if (!text) return
  adding.value = ''
  save([...props.todos, { id: Date.now().toString(36) + Math.random().toString(36).slice(2, 6), text, done: false }])
}
function grow(el: HTMLTextAreaElement) {
  el.style.height = 'auto'
  el.style.height = el.scrollHeight + 'px'
}

// ---- reordering: drag an item's grip ----
const list = ref<HTMLElement>()
const dragging = ref('')
const over = ref<{ id: string; after: boolean } | null>(null)
function startDrag(e: PointerEvent, t: Todo) {
  if (e.button !== 0) return
  e.preventDefault()
  dragging.value = t.id
  const move = (m: PointerEvent) => {
    const items = [...(list.value?.querySelectorAll<HTMLElement>('li.item') ?? [])]
    const before = items.find((el) => m.clientY < el.getBoundingClientRect().top + el.offsetHeight / 2)
    const last = items[items.length - 1]
    over.value = before ? { id: before.dataset.id!, after: false } : last ? { id: last.dataset.id!, after: true } : null
  }
  const up = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    drop()
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}
function drop() {
  const id = dragging.value
  const o = over.value
  dragging.value = ''
  over.value = null
  const moving = props.todos.find((x) => x.id === id)
  if (!o || !moving || o.id === id) return
  const rest = props.todos.filter((x) => x.id !== id)
  rest.splice(rest.findIndex((x) => x.id === o.id) + (o.after ? 1 : 0), 0, moving)
  if (rest.some((x, i) => x.id !== props.todos[i].id)) save(rest)
}
const dropCls = (t: Todo) => (over.value?.id === t.id && dragging.value !== t.id ? (over.value.after ? 'drop-after' : 'drop-before') : '')
</script>

<template>
  <ul ref="list" class="todos" :class="{ sorting: dragging }">
    <template v-for="t in todos" :key="t.id">
      <li v-if="shown(t)" class="item" :data-id="t.id" :class="[{ done: t.done, lifted: dragging === t.id, urgent: t.urgent }, dropCls(t)]">
        <span class="grip" title="Drag to move" @pointerdown="startDrag($event, t)"><Icon name="grip" :size="13" /></span>
        <button class="box" role="checkbox" :aria-checked="t.done" :aria-label="t.done ? 'Mark as not done' : 'Mark as done'" @click="patch(t.id, { done: !t.done })">
          <Icon v-if="t.done" name="check" :size="11" :stroke="3" />
        </button>
        <textarea
          v-if="editing === t.id"
          ref="editor"
          v-model="draft"
          class="edit"
          rows="1"
          data-own-escape
          @input="grow($event.target as HTMLTextAreaElement)"
          @keydown="editKey"
          @blur="commit"
        ></textarea>
        <span v-else class="text" title="Click to edit" @click="edit(t)">{{ t.text }}</span>
        <button class="urg" :aria-pressed="!!t.urgent" :aria-label="t.urgent ? 'Urgent: unmark it' : 'Mark as urgent'" :title="t.urgent ? 'Urgent: click to unmark' : 'Mark as urgent'" @click="patch(t.id, { urgent: !t.urgent })">
          <Icon name="warn" :size="13" />
        </button>
        <button class="x" aria-label="Delete" title="Delete" @click="save(todos.filter((x) => x.id !== t.id))"><Icon name="close" :size="12" /></button>
      </li>
    </template>
    <li class="add">
      <Icon name="plus" :size="13" />
      <input v-model="adding" placeholder="Add a todo" @keydown.enter="add" />
    </li>
  </ul>
</template>

<style scoped>
.todos {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
}
.todos.sorting {
  cursor: grabbing;
  user-select: none;
}
.item,
.add {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 4px 6px 4px 2px;
  border-radius: 5px;
  font-size: 13px;
  line-height: 1.45;
}
.todos:not(.sorting) .item:hover {
  background: var(--hover);
}
.item.lifted {
  opacity: 0.45;
}
/* where the dragged todo lands: a straight line above or below the item */
.item.drop-before::before,
.item.drop-after::after {
  content: '';
  position: absolute;
  left: 47px; /* where the text starts: padding, grip, checkbox and their gaps */
  right: 62px; /* where it ends: the urgent and delete buttons */
  height: 1px;
  background: var(--text);
}
.item.drop-before::before {
  top: 0;
}
.item.drop-after::after {
  bottom: 0;
}
.grip {
  flex: none;
  display: grid;
  place-items: center;
  width: 14px;
  height: 20px;
  color: var(--faint);
  cursor: grab;
  visibility: hidden;
}
.box {
  flex: none;
  display: grid;
  place-items: center;
  width: 15px;
  height: 15px;
  margin-top: 2px;
  padding: 0;
  border: 1.5px solid var(--faint);
  border-radius: 4px;
  background: transparent;
  color: var(--button-text);
}
.done .box {
  border-color: var(--done-dot);
  background: var(--done-dot);
}
.text {
  flex: 1;
  min-width: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  cursor: text;
}
.done .text {
  color: var(--muted);
  text-decoration: line-through;
}
.edit {
  flex: 1;
  min-width: 0;
  margin: -2px 0;
  padding: 1px 4px;
  border: 1px solid var(--accent);
  border-radius: 4px;
  background: var(--surface);
  color: var(--text);
  font: inherit;
  resize: none;
  outline: none;
  overflow: hidden;
}
.urg,
.x {
  flex: none;
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--muted);
  visibility: hidden;
}
.urgent:not(.done) .text {
  color: var(--urgent);
}
.urgent .urg {
  color: var(--urgent);
}
.todos:not(.sorting) .item:hover :is(.grip, .urg, .x) {
  visibility: visible;
}
.urg:hover {
  color: var(--urgent);
}
.x:hover {
  color: var(--approval);
}
.add {
  align-items: center;
  padding-left: 25px;
  color: var(--faint);
}
.add input {
  flex: 1;
  min-width: 0;
  padding: 2px 0;
  border: 0;
  background: transparent;
  color: var(--text);
  font: inherit;
  outline: none;
}
</style>
