<script setup lang="ts">
import { computed } from 'vue'
import { herd } from '../lib/state'
import Icon from './ui/Icon.vue'
import Kbd from './ui/Kbd.vue'
import TaskRef from './ui/TaskRef.vue'
import StatusBadge from './ui/StatusBadge.vue'
import NextStep from './NextStep.vue'
import Splitter from './ui/Splitter.vue'

const counts = computed(() => ({
  active: herd.tasks.filter((t) => !t.archived).length,
  archived: herd.tasks.filter((t) => t.archived).length,
}))
const groups = computed(() =>
  herd.projects.map((p) => ({
    p,
    tasks: herd.tasks.filter((t) => t.project === p.id && (herd.left === 'archive' ? t.archived : !t.archived)),
  })),
)
const toggle = (id: string) => (herd.collapsed[id] = !herd.collapsed[id])
function addProject() {
  herd.view = 'settings'
  herd.settingsTab = 'projects'
}
</script>

<template>
  <aside :style="{ width: herd.leftWidth + 'px' }">
    <Splitter side="left" />
    <div class="head">
      <button class="tab" :class="{ on: herd.left === 'tasks' }" @click="herd.left = 'tasks'">Tasks <span class="muted">{{ counts.active }}</span></button>
      <button class="tab" :class="{ on: herd.left === 'archive' }" @click="herd.left = 'archive'">Archive <span class="muted">{{ counts.archived }}</span></button>
      <span class="grow"></span>
      <Kbd action="toggleArchive" />
    </div>

    <div class="list">
      <template v-for="g in groups" :key="g.p.id">
        <button class="group" :aria-expanded="!herd.collapsed[g.p.id]" @click="toggle(g.p.id)">
          <Icon :name="herd.collapsed[g.p.id] ? 'chevron-right' : 'chevron'" :size="11" :stroke="2.4" />
          <Icon name="folder" />
          <span class="name">{{ g.p.name }}</span>
          <span>{{ g.tasks.length }}</span>
        </button>
        <template v-if="!herd.collapsed[g.p.id]">
          <span v-if="!g.tasks.length" class="empty">{{ herd.left === 'archive' ? 'Nothing archived' : 'No open tasks' }}</span>
          <button v-for="t in g.tasks" :key="t.id" class="card" :class="{ on: t.id === herd.sel }" @click="herd.pickTask(t.id)">
            <span class="row">
              <TaskRef :issue="t.issue" />
              <span class="title">{{ t.title }}</span>
            </span>
            <StatusBadge :kind="t.status.kind" :label="t.status.label" />
          </button>
        </template>
      </template>
      <div v-if="!groups.length" class="none">
        <p>No projects yet.</p>
        <button class="btn" @click="addProject">Add a project</button>
      </div>
    </div>

    <NextStep />
  </aside>
</template>

<style scoped>
aside {
  position: relative;
  flex: none;
  display: flex;
  flex-direction: column;
  background: var(--bg);
  border-right: 1px solid var(--border);
  min-height: 0;
}
.head {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
  height: 34px;
  padding: 0 6px;
  border-bottom: 1px solid var(--border);
}
.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 8px;
  border-radius: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
}
.tab.on {
  color: var(--text);
  background: var(--hover);
}
.grow {
  flex: 1;
}
.list {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  padding: 4px 6px 8px;
}
.group {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  flex: none;
  padding: 0 6px;
  margin-top: 4px;
  border: 0;
  background: transparent;
  font-size: 12px;
  color: var(--muted);
  text-align: left;
}
.group .name {
  color: var(--text);
  font-weight: 600;
  font-size: 12.5px;
}
.empty {
  padding: 2px 12px 6px 30px;
  font-size: 12px;
  color: var(--muted);
}
.card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  flex: none;
  text-align: left;
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid transparent;
  background: transparent;
  font-size: 13px;
  color: var(--text);
}
.card:hover {
  background: var(--hover);
}
.card.on {
  border-color: var(--focus);
  background: var(--sel);
}
.row {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  width: 100%;
}
.title {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.none {
  padding: 16px 10px;
  color: var(--muted);
}
.none p {
  margin: 0 0 10px;
}
</style>
