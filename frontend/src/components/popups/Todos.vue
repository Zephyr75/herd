<script setup lang="ts">
// Every todo list: the open tasks with todos (and the selected task), the
// archived ones that still have open todos, then Others, for the todos of
// no task. A task's name opens it.
import { computed } from 'vue'
import { herd } from '../../lib/state'
import type { Task, Todo } from '../../lib/types'
import Icon from '../ui/Icon.vue'
import TaskRef from '../ui/TaskRef.vue'
import TodoList from '../TodoList.vue'

const open = (ts: Todo[]) => ts.filter((x) => !x.done).length
const tasks = computed(() => {
  const shown = herd.tasks.filter((t) => t.id === herd.sel || (t.archived ? open(t.todos) > 0 : t.todos.length > 0))
  return shown.sort((a, b) => Number(a.archived) - Number(b.archived) || a.projectName.localeCompare(b.projectName) || a.id.localeCompare(b.id))
})
const total = computed(() => open(herd.todos) + herd.tasks.reduce((n, t) => n + open(t.todos), 0))

function go(t: Task) {
  herd.overlay = 'none'
  herd.left = t.archived ? 'archive' : 'tasks'
  herd.pickTask(t.id)
}
</script>

<template>
  <button class="scrim dim" aria-label="Close the todo lists" @click="herd.overlay = 'none'"></button>
  <div class="box popup" role="dialog" aria-label="Todo lists">
    <div class="head">
      <Icon name="todo" :size="15" />
      <span class="title">Todos</span>
      <span class="muted small">{{ total }} open</span>
      <span class="grow"></span>
      <label class="hide small"><input v-model="herd.hideDone" type="checkbox" />Hide done</label>
      <button class="x" aria-label="Close" @click="herd.overlay = 'none'"><Icon name="close" :size="14" /></button>
    </div>
    <div class="body">
      <section v-for="t in tasks" :key="t.id">
        <button class="task" :title="`Open ${t.title}`" @click="go(t)">
          <TaskRef :issue="t.issue" :task="t.id" />
          <span class="name">{{ t.title }}</span>
          <span class="muted small">{{ t.projectName }}{{ t.archived ? ' · archived' : '' }}</span>
        </button>
        <TodoList :todos="t.todos" :hide-done="herd.hideDone" @change="herd.setTodos(t, $event)" />
      </section>
      <section>
        <span class="task static"><span class="name">Others</span><span class="muted small">not related to a task</span></span>
        <TodoList :todos="herd.todos" :hide-done="herd.hideDone" @change="herd.setTodos(null, $event)" />
      </section>
    </div>
  </div>
</template>

<style scoped>
.box {
  position: absolute;
  z-index: 21;
  /* like the artifact and diff pop-ups (ViewPopups) */
  top: 48px;
  bottom: 40px;
  left: max(24px, calc(50% - 700px));
  right: max(24px, calc(50% - 700px));
  display: flex;
  flex-direction: column;
}
.head {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 42px;
  flex: none;
  padding: 0 10px 0 14px;
  border-bottom: 1px solid var(--border);
}
.title {
  font-weight: 600;
}
.grow {
  flex: 1;
}
.small {
  font-size: 12px;
}
.hide {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
}
.x {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--muted);
}
.x:hover {
  background: var(--hover);
  color: var(--text);
}
.body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 12px 10px 16px;
}
.task {
  display: flex;
  align-items: baseline;
  gap: 8px;
  width: 100%;
  padding: 4px 6px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--text);
  text-align: left;
}
button.task:hover {
  background: var(--hover);
}
.task .name {
  font-weight: 600;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}
.task .small {
  flex: none;
}
</style>
