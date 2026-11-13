<script setup lang="ts">
// Preview full context: a popup with everything a step of the agent gets, as
// being edited, for an example task of the chosen project.
import { computed, nextTick, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import type { Agent, ContextPart } from '../../lib/types'
import Icon from '../ui/Icon.vue'

const props = defineProps<{ agent: Agent }>()

const parts = ref<ContextPart[] | null>(null) // null: closed
const project = ref('')
const box = ref<HTMLElement | null>(null)
const projects = computed(() => herd.settings?.config.projects ?? [])
const load = debounce(async () => {
  if (parts.value === null) return
  parts.value = (await herd.attempt('Could not build the context', () => api.AgentContext(JSON.parse(JSON.stringify(props.agent)), project.value))) ?? []
}, 300)
async function open() {
  parts.value = []
  load()
  await nextTick()
  box.value?.focus()
}
const close = () => (parts.value = null)
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
  }
}
watch(() => [JSON.stringify(props.agent), JSON.stringify(herd.settings?.prompts), JSON.stringify(herd.settings?.config), project.value], load)
</script>

<template>
  <div class="ctx">
    <button class="btn" @click="open"><Icon name="eye" :size="13" />Preview full context</button>
    <span class="hint">Everything a step of {{ agent.name }} gets, in the order Claude reads it.</span>
  </div>
  <Teleport v-if="parts !== null" to="body">
    <button class="scrim dim" aria-label="Close the context preview" @click="close"></button>
    <div ref="box" class="box popup" role="dialog" :aria-label="`Full context of ${agent.name}`" tabindex="-1" data-own-escape @keydown="key">
      <div class="head">
        <span class="title">Full context of {{ agent.name }}</span>
        <span class="grow"></span>
        <label v-if="projects.length > 1" class="proj">
          <span class="muted">Example task of</span>
          <select v-model="project" aria-label="Project of the example task">
            <option value="">{{ projects[0].name }}</option>
            <option v-for="p in projects.slice(1)" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </label>
        <button class="x" aria-label="Close" @click="close"><Icon name="close" :size="14" /></button>
      </div>
      <div class="body">
        <span v-if="!parts.length" class="muted">Building the context…</span>
        <details v-for="p in parts" :key="p.title" class="part" open>
          <summary>
            <span class="title mono">{{ p.title }}</span>
            <span class="where">{{ p.where }}</span>
          </summary>
          <pre class="text mono">{{ p.text }}</pre>
        </details>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ctx {
  display: flex;
  align-items: center;
  gap: 10px;
}
.ctx .btn {
  gap: 6px;
  height: 28px;
}
.hint {
  font-size: 12px;
  color: var(--muted);
}
.box {
  position: fixed;
  z-index: 21;
  top: 48px;
  bottom: 48px;
  left: 50%;
  transform: translateX(-50%);
  width: 860px;
  max-width: calc(100% - 32px);
  display: flex;
  flex-direction: column;
  outline: none;
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 42px;
  flex-shrink: 0;
  padding: 0 10px 0 14px;
  border-bottom: 1px solid var(--border);
}
.head .title {
  font-weight: 600;
}
.grow {
  flex: 1;
}
.proj {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.proj select {
  font-size: 12px;
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
  background: var(--hover, var(--surface));
  color: var(--text);
}
.body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
}
.part summary {
  display: flex;
  align-items: baseline;
  gap: 8px;
  cursor: pointer;
  padding: 4px 0;
}
.part .title {
  font-size: 12px;
  font-weight: 600;
}
.where {
  font-size: 11.5px;
  color: var(--muted);
}
.text {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
  font-size: 11.5px;
  line-height: 1.5;
  white-space: pre-wrap;
}
</style>
