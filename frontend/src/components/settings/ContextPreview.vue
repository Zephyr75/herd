<script setup lang="ts">
// Preview full context: everything a step of the agent gets, as being
// edited, for an example task of the chosen project.
import { computed, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import type { Agent, ContextPart } from '../../lib/types'

const props = defineProps<{ agent: Agent }>()

const parts = ref<ContextPart[] | null>(null) // null: closed
const project = ref('')
const projects = computed(() => herd.settings?.config.projects ?? [])
const load = debounce(async () => {
  if (parts.value === null) return
  parts.value = (await herd.attempt('Could not build the context', () => api.AgentContext(JSON.parse(JSON.stringify(props.agent)), project.value))) ?? []
}, 300)
function toggle() {
  parts.value = parts.value === null ? [] : null
  load()
}
watch(() => [JSON.stringify(props.agent), JSON.stringify(herd.settings?.prompts), JSON.stringify(herd.settings?.config), project.value], load)
</script>

<template>
  <div class="ctx">
    <div class="row">
      <button class="linkish" @click="toggle">{{ parts === null ? 'Preview full context' : 'Hide full context' }}</button>
      <select v-if="parts !== null && projects.length > 1" v-model="project" class="proj" aria-label="Project of the example task">
        <option value="">{{ projects[0].name }}</option>
        <option v-for="p in projects.slice(1)" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
    </div>
    <span class="hint">Everything a step of {{ agent.name }} gets, in the order Claude reads it, for an example task of the project.</span>
    <details v-for="p in parts ?? []" :key="p.title" class="part" open>
      <summary>
        <span class="title mono">{{ p.title }}</span>
        <span class="where">{{ p.where }}</span>
      </summary>
      <pre class="text mono">{{ p.text }}</pre>
    </details>
  </div>
</template>

<style scoped>
.ctx {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.linkish {
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--accent, var(--text));
  font-size: 12.5px;
  text-decoration: underline;
}
.proj {
  font-size: 12px;
}
.hint {
  font-size: 12px;
  color: var(--muted);
}
.part summary {
  display: flex;
  align-items: baseline;
  gap: 8px;
  cursor: pointer;
  padding: 4px 0;
}
.title {
  font-size: 12px;
  font-weight: 600;
}
.where {
  font-size: 11.5px;
  color: var(--muted);
}
.text {
  max-height: 420px;
  overflow: auto;
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
