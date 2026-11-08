<script setup lang="ts">
// Settings > Context > Prompts: the texts herd adds to agents (around their
// instructions) and to the fix steps it starts from the PR tab.
import { reactive } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'

const savers: Record<string, (t: string) => void> = {}
const drafts = reactive<Record<string, string>>({})
function edit(id: string, e: Event) {
  const text = (e.target as HTMLTextAreaElement).value
  drafts[id] = text
  savers[id] ??= debounce((t: string) => void herd.attempt('Could not save the prompt', () => api.SetPrompt(id, t)), 500)
  savers[id](text)
}
async function reset(id: string) {
  delete drafts[id]
  await herd.attempt('Could not reset the prompt', () => api.SetPrompt(id, ''))
}
const textOf = (id: string, text: string) => drafts[id] ?? text
</script>

<template>
  <div class="prompts">
    <p class="muted small">What herd adds to every agent's command, around its instructions (Show full command in Settings › Agents shows the result), and the guidance of the steps the PR tab starts. Words in braces are replaced by herd; an emptied prompt goes back to its default.</p>
    <div v-for="p in herd.settings?.prompts ?? []" :key="p.id" class="editor card">
      <div class="title">
        <span class="name">{{ p.name }}</span>
        <span class="muted small">{{ p.where }}</span>
        <span class="muted small mono">{{ p.vars }}</span>
      </div>
      <label :for="'prompt-' + p.id" class="sr-only">{{ p.name }}</label>
      <textarea :id="'prompt-' + p.id" class="area mono" :rows="Math.min(14, textOf(p.id, p.text).split('\n').length + 1)" :value="textOf(p.id, p.text)" @input="edit(p.id, $event)"></textarea>
      <div v-if="textOf(p.id, p.text).trim() !== p.default.trim()" class="row">
        <span class="muted small">Edited</span>
        <button class="btn ghost" @click="reset(p.id)">Reset to default</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .prompts {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 18px;
  }
  .title {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .name {
    font-weight: 600;
  }
  .small {
    font-size: 12px;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
  }
  p {
    margin: 0;
  }
</style>
