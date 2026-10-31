<script setup lang="ts">
// Settings > Context: general context plus one block per project.
import { computed, ref } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'

const selId = ref('general')
const keys = computed(() => ['general', ...herd.projects.map((p) => p.id)])
const proj = computed(() => herd.project(selId.value))
const C = computed(
  () => herd.settings?.context[selId.value] ?? { id: selId.value, text: '', agents: herd.settings?.agents.map((a) => a.id) ?? [], claudeMd: true },
)

const save = debounce((id: string) => {
  const c = herd.settings?.context[id]
  if (c) void herd.attempt('Could not save the context', () => api.SaveContext(JSON.parse(JSON.stringify(c))))
}, 500)

function ensure() {
  const s = herd.settings
  if (!s) return null
  if (!s.context[selId.value]) s.context[selId.value] = { ...C.value, id: selId.value }
  return s.context[selId.value]
}
function update(fn: (c: NonNullable<ReturnType<typeof ensure>>) => void) {
  const c = ensure()
  if (!c) return
  fn(c)
  herd.edited()
  save(selId.value)
}
const setText = (e: Event) => update((c) => (c.text = (e.target as HTMLTextAreaElement).value))
const toggleMd = () => update((c) => (c.claudeMd = !c.claudeMd))
const toggleAgent = (id: string) =>
  update((c) => {
    const list = c.agents ?? []
    c.agents = list.includes(id) ? list.filter((x) => x !== id) : [...list, id]
  })
</script>

<template>
  <div class="intro">
    <h1>Context</h1>
    <p>Markdown given to every session. General context applies everywhere; project context is added on top for tasks in that project.</p>
  </div>

  <div class="split">
    <div class="list">
      <button v-for="k in keys" :key="k" class="list-btn" :class="{ on: selId === k }" @click="selId = k">
        <span class="name">{{ herd.project(k)?.name ?? 'General' }}</span>
        <span class="sub">{{ herd.project(k) ? 'project context' : 'every project' }}</span>
      </button>
    </div>
    <div class="editor card">
      <div class="title">
        <span class="name">{{ proj ? proj.name : 'General context' }}</span>
        <span class="muted small">{{ proj ? `Added for tasks in ${proj.name}, after the general context` : 'Given to every agent in every project' }}</span>
      </div>
      <label for="ctxtext" class="sr-only">Context markdown</label>
      <textarea id="ctxtext" class="area mono" rows="12" :value="C.text" @input="setText"></textarea>
      <div v-if="proj" class="row">
        <input id="claudemd" type="checkbox" :checked="C.claudeMd" @change="toggleMd" />
        <label for="claudemd">Also load the repo's <span class="mono">CLAUDE.md</span></label>
      </div>
      <div class="row wrap">
        <span class="muted small">Applies to</span>
        <button v-for="a in herd.settings?.agents ?? []" :key="a.id" class="seg" :class="{ on: (C.agents ?? []).includes(a.id) }" @click="toggleAgent(a.id)">{{ a.name }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .split {
    display: flex;
    gap: 18px;
    flex-wrap: wrap;
    align-items: flex-start;
  }
  .list {
    flex: 1 1 200px;
    max-width: 230px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name {
    font-weight: 500;
  }
  .editor {
    flex: 999 1 420px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px 18px;
  }
  .title {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .title .name {
    font-weight: 600;
  }
  .small {
    font-size: 12px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .row.wrap {
    flex-wrap: wrap;
    gap: 6px;
  }
</style>
