<script setup lang="ts">
// Settings > Context: the Rules every agent follows (unless it ignores them),
// then general context plus one block per project, each with an optional
// knowledge base folder.
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../../lib/api'
import { debounce } from '../../lib/debounce'
import { herd } from '../../lib/state'
import Icon from '../ui/Icon.vue'
import type { SystemMemory } from '../../lib/types'

const selId = ref('general')
const keys = computed(() => ['general', ...herd.projects.map((p) => p.id)])
const proj = computed(() => herd.project(selId.value))
const C = computed(
  () => herd.settings?.context[selId.value] ?? { id: selId.value, text: '', claudeMd: selId.value !== 'general', knowledge: '' },
)

// Rules go in each session's system prompt, unlike context (CONTEXT.md).
const ignoring = computed(() => (herd.settings?.agents ?? []).filter((a) => a.ignoreRules).map((a) => a.name))
const saveRules = debounce((r: string) => void herd.attempt('Could not save the rules', () => api.SetRules(r)), 500)
function setRules(e: Event) {
  const c = herd.settings?.config
  if (!c) return
  c.rules = (e.target as HTMLTextAreaElement).value
  herd.edited()
  saveRules(c.rules)
}

// Your own ~/.claude/CLAUDE.md is not loaded by herd's sessions; Override
// copies General context + Rules into it to keep the two in sync.
const sys = ref<SystemMemory | null>(null)
const confirmOverride = ref(false)
const refreshSys = debounce(async () => {
  sys.value = await api.SystemClaudeMd().catch(() => null)
}, 600)
onMounted(refreshSys)
watch(() => [herd.settings?.config.rules, JSON.stringify(herd.settings?.context)], refreshSys)
async function override() {
  if (!confirmOverride.value) {
    confirmOverride.value = true
    return
  }
  confirmOverride.value = false
  const backup = await herd.attempt('Could not write ' + (sys.value?.path ?? 'CLAUDE.md'), () => api.OverrideSystemClaudeMd())
  if (backup === undefined) return
  herd.toast('Updated ' + (sys.value?.path ?? 'CLAUDE.md'), backup ? 'The previous file is at ' + backup : '')
  refreshSys()
}

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
async function pickKnowledge() {
  const dir = await herd.attempt('Could not open the folder picker', () => api.ChooseFolder('Knowledge base folder'))
  if (dir) update((c) => (c.knowledge = dir))
}
const clearKnowledge = () => update((c) => (c.knowledge = ''))
</script>

<template>
  <div class="intro">
    <h1>Context</h1>
    <p>Rules are always followed by every agent, except those set to ignore them. Context is markdown given to every session: general context applies everywhere, project context is added on top for tasks in that project. Sessions do not load your own <span class="mono">~/.claude/CLAUDE.md</span>: put what agents need here.</p>
  </div>

  <div class="split">
    <div class="list">
      <button class="list-btn" :class="{ on: selId === 'rules' }" @click="selId = 'rules'">
        <span class="name">Rules</span>
        <span class="sub">every agent, always</span>
      </button>
      <button v-for="k in keys" :key="k" class="list-btn" :class="{ on: selId === k }" @click="selId = k">
        <span class="name">{{ herd.project(k)?.name ?? 'General' }}</span>
        <span class="sub">{{ herd.project(k) ? 'project context' : 'every project' }}</span>
      </button>
    </div>
    <div class="col">
      <div v-if="selId === 'rules'" class="editor card">
        <div class="title">
          <span class="name">Rules</span>
          <span class="muted small">
            In every agent's system prompt, e.g. what it may and may not do.
            {{ ignoring.length ? `Ignored by ${ignoring.join(', ')} (Settings > Agents).` : 'An agent can ignore them in Settings > Agents.' }}
          </span>
        </div>
        <label for="rulestext" class="sr-only">Rules</label>
        <textarea id="rulestext" class="area mono" rows="12" :value="herd.settings?.config.rules ?? ''" placeholder="e.g. Never run git commit or git push&#10;Never touch migrations/" @input="setRules"></textarea>
      </div>
      <div v-else class="editor card">
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
        <div class="field">
          <span>Knowledge base folder <span class="muted small">readable by every session, for background and docs</span></span>
          <div class="row">
            <button class="btn folder" :title="C.knowledge || 'Choose a folder'" @click="pickKnowledge">
              <Icon name="folder" /><span class="path mono">{{ C.knowledge || 'Choose a folder…' }}</span>
            </button>
            <button v-if="C.knowledge" class="btn ghost" @click="clearKnowledge">Clear</button>
          </div>
        </div>
      </div>
      <div v-if="sys && (selId === 'rules' || selId === 'general')" class="sys card">
        <span class="grow">
          <span class="mono">{{ sys.path }}</span>
          <span :class="['state', sys.synced ? 'c-done' : 'c-waiting']">{{ sys.synced ? 'in sync' : sys.exists ? 'differs' : 'missing' }}</span>
          <span class="muted small">herd's sessions skip it; Override writes General context, Rules and the knowledge base folders into it for your other Claude sessions</span>
        </span>
        <button class="btn" :class="confirmOverride ? 'danger' : ''" :disabled="sys.synced" @click="override" @blur="confirmOverride = false">
          {{ confirmOverride ? 'Replace it? (a backup is kept)' : 'Override' }}
        </button>
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
  .folder {
    flex: 1;
    min-width: 0;
    justify-content: flex-start;
    gap: 8px;
  }
  .path {
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .col {
    flex: 999 1 420px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .col > .editor {
    flex: none;
  }
  .sys {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
  }
  .sys .grow {
    flex: 1;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 4px 10px;
    font-size: 12.5px;
  }
  .state {
    font-size: 12px;
  }
  .c-done { color: var(--done); }
  .c-waiting { color: var(--waiting); }
</style>
